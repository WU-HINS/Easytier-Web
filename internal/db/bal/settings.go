package bal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

// Setting is one row of the key/value configuration table.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// GetSetting reads one configuration value. Missing keys return ErrNotFound so
// callers can distinguish "unset" from "empty string".
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	statement, args := query.From("settings").Columns("value").
		Where("key", "=", key).Build(s.Dialect())
	var value string
	err := s.conn.QueryRow(ctx, statement, args...).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("bal: get setting: %w", err)
	}
	return value, nil
}

// GetSettingDefault reads a setting, falling back when it is unset.
func (s *Store) GetSettingDefault(ctx context.Context, key, fallback string) string {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return fallback
	}
	return value
}

// PutSetting writes one configuration value.
func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	statement, args := query.Into("settings").
		Columns("key", "value", "updated_at").
		Values(key, value, dal.TimeArg(s.Dialect(), time.Now())).
		OnConflict([]string{"key"}, []string{"value", "updated_at"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: put setting: %w", err)
	}
	return nil
}

// ErrSettingsConflict is returned when a caller writes settings using a stale
// revision.
type ErrSettingsConflict struct {
	Current int
}

func (e *ErrSettingsConflict) Error() string {
	return fmt.Sprintf("bal: settings changed (current revision %d)", e.Current)
}

// settingsRevisionKey mirrors settings.KeyRevision; it lives here so BAL does
// not depend on the higher level settings package.
const settingsRevisionKey = "settings.revision"

// SettingsRevision returns the current settings revision (0 when never written).
func (s *Store) SettingsRevision(ctx context.Context) (int, error) {
	statement, args := query.From("settings").Columns("value").
		Where("key", "=", settingsRevisionKey).Build(s.Dialect())
	var raw string
	err := s.conn.QueryRow(ctx, statement, args...).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("bal: read revision: %w", err)
	}
	value, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 0, fmt.Errorf("bal: corrupt revision %q", raw)
	}
	return value, nil
}

func revisionUpdateStatement(dialect dal.Dialect) string {
	placeholder := func(index int) string { return query.Placeholder(dialect, index) }
	return "UPDATE " + query.Quote("settings") +
		" SET " + query.Quote("value") + " = " + placeholder(1) +
		", " + query.Quote("updated_at") + " = " + placeholder(2) +
		" WHERE " + query.Quote("key") + " = " + placeholder(3) +
		" AND " + query.Quote("value") + " = " + placeholder(4)
}

// PutSettings writes several keys in one transaction and bumps the revision.
//
// expected is the revision the caller based its decision on. When it does not
// match, nothing is written and ErrSettingsConflict is returned, so two admins
// editing at the same time get a conflict instead of silently overwriting each
// other. A nil expected means "write unconditionally" (used by setup paths).
func (s *Store) PutSettings(ctx context.Context, values map[string]string, expected *int) (int, error) {
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("bal: begin settings: %w", err)
	}
	now := dal.TimeArg(s.Dialect(), time.Now())

	current := 0
	readStatement, readArgs := query.From("settings").Columns("value").
		Where("key", "=", settingsRevisionKey).Build(s.Dialect())
	var rawRevision string
	switch err := tx.QueryRow(ctx, readStatement, readArgs...).Scan(&rawRevision); {
	case err == nil:
		parsed, convErr := strconv.Atoi(rawRevision)
		if convErr != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("bal: corrupt revision %q", rawRevision)
		}
		current = parsed
	case errors.Is(err, sql.ErrNoRows):
		// First write ever. Seeding has to be idempotent: two admins saving for the
		// first time at the same moment both find no row, and the loser used to get a
		// 500 with the driver text in it.
		insertRevision, insertArgs := query.Into("settings").
			Columns("key", "value", "updated_at").
			Values(settingsRevisionKey, "0", now).
			OnConflict([]string{"key"}, nil).Build(s.Dialect())
		if _, insertErr := tx.Exec(ctx, insertRevision, insertArgs...); insertErr != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("bal: seed revision: %w", insertErr)
		}
		// Read it back: the row may be ours or the other writer's.
		if readErr := tx.QueryRow(ctx, readStatement, readArgs...).Scan(&rawRevision); readErr != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("bal: reread revision: %w", readErr)
		}
		parsed, convErr := strconv.Atoi(rawRevision)
		if convErr != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("bal: corrupt revision %q", rawRevision)
		}
		current = parsed
	default:
		_ = tx.Rollback()
		return 0, fmt.Errorf("bal: read revision: %w", err)
	}

	if expected != nil && *expected != current {
		_ = tx.Rollback()
		return current, &ErrSettingsConflict{Current: current}
	}

	next := current + 1
	affected, err := tx.Exec(ctx, revisionUpdateStatement(s.Dialect()),
		fmt.Sprintf("%d", next), now, settingsRevisionKey, fmt.Sprintf("%d", current))
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("bal: bump revision: %w", err)
	}
	if affected == 0 {
		// Another writer committed between our read and this update.
		_ = tx.Rollback()
		latest, readErr := s.SettingsRevision(ctx)
		if readErr != nil {
			latest = current
		}
		return latest, &ErrSettingsConflict{Current: latest}
	}

	for key, value := range values {
		if key == settingsRevisionKey {
			continue
		}
		statement, args := query.Into("settings").
			Columns("key", "value", "updated_at").
			Values(key, value, now).
			OnConflict([]string{"key"}, []string{"value", "updated_at"}).
			Build(s.Dialect())
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("bal: put setting %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("bal: commit settings: %w", err)
	}
	return next, nil
}

// ListSettings returns every configuration row (secrets included; the caller
// decides what to redact).
func (s *Store) ListSettings(ctx context.Context) ([]Setting, error) {
	statement, args := query.From("settings").
		Columns("key", "value", "updated_at").OrderBy("key", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list settings: %w", err)
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var (
			setting   Setting
			updatedAt any
		)
		if err := rows.Scan(&setting.Key, &setting.Value, &updatedAt); err != nil {
			return nil, fmt.Errorf("bal: scan setting: %w", err)
		}
		if setting.UpdatedAt, err = dal.ParseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, setting)
	}
	return out, rows.Err()
}
