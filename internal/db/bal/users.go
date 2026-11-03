package bal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

const userColumns = "id, username, password, email, oidc_subject, oidc_issuer, display_name, role, disabled, created_at"

// CreateUser inserts an account with a local password hash.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	now := time.Now()
	// RETURNING works on SQLite 3.35+ and PostgreSQL alike, and is the only
	// portable way to learn the new primary key: Conn.Exec reports rows
	// affected, never the insert id.
	statement, args := query.Into("users").
		Columns("username", "password", "role", "created_at").
		Values(username, passwordHash, RoleUser, dal.TimeArg(s.Dialect(), now)).
		Build(s.Dialect())
	statement += " RETURNING id"
	var id int64
	if err := s.conn.QueryRow(ctx, statement, args...).Scan(&id); err != nil {
		// A duplicate name is a user facing outcome, not a server error: two
		// concurrent registrations both pass a caller side check, and the unique
		// index rejects the loser. Map it here so no driver text escapes.
		if isUsernameViolation(err) {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("bal: create user: %w", err)
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash, Role: RoleUser, CreatedAt: now}, nil
}

// UpsertOIDCUser links (or creates) an account for a verified OIDC identity.
// ErrUsernameTaken means every candidate name the caller suggested was already in
// use. Callers normally never see it: UpsertOIDCUser walks the suffixes itself.
var ErrUsernameTaken = errors.New("bal: username is already taken")

// isUsernameViolation reports a clash on users.username specifically, so a
// collision can be retried with another name instead of failing the login.
func isUsernameViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "users.username") ||
		strings.Contains(message, "uq_users_username")
}

// UpsertOIDCUser maps an identity to an account, picking a free username when the
// preferred one is taken. The retry lives here, not in the caller: two first time
// SSO logins that share a preferred_username would otherwise both pick the same
// free name and one would fail on the unique index.
func (s *Store) UpsertOIDCUser(ctx context.Context, issuer, subject, username, email, displayName string) (User, error) {
	candidate := username
	for attempt := 0; attempt < 20; attempt++ {
		user, err := s.upsertOIDCUserOnce(ctx, issuer, subject, candidate, email, displayName)
		if err == nil {
			return user, nil
		}
		if !isUsernameViolation(err) {
			return User{}, err
		}
		candidate = fmt.Sprintf("%s-%d", username, attempt+2)
	}
	return User{}, ErrUsernameTaken
}

func (s *Store) upsertOIDCUserOnce(ctx context.Context, issuer, subject, username, email, displayName string) (User, error) {
	now := time.Now()
	statement, args := query.Into("users").
		Columns("username", "password", "email", "oidc_subject", "oidc_issuer", "display_name", "created_at").
		Values(username, "", email, subject, issuer, displayName, dal.TimeArg(s.Dialect(), now)).
		OnConflict([]string{"oidc_issuer", "oidc_subject"}, []string{"username", "email", "display_name"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return User{}, fmt.Errorf("bal: upsert oidc user: %w", err)
	}
	return s.GetUserByOIDC(ctx, issuer, subject)
}

func (s *Store) scanUser(row dal.Row) (User, error) {
	var (
		user                                User
		email, subject, issuer, displayName sql.NullString
		role                                string
		disabled                            any
		created                             any
	)
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &email, &subject, &issuer, &displayName, &role, &disabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("bal: scan user: %w", err)
	}
	user.Email = email.String
	user.OIDCSubject = subject.String
	user.OIDCIssuer = issuer.String
	user.DisplayName = displayName.String
	user.Role = role
	user.Disabled = dal.ParseBool(disabled)
	if user.CreatedAt, err = dal.ParseTime(created); err != nil {
		return User{}, err
	}
	return user, nil
}

// GetUserByName looks an account up by username.
func (s *Store) GetUserByName(ctx context.Context, username string) (User, error) {
	statement, args := query.From("users").Columns(splitColumns(userColumns)...).
		Where("username", "=", username).Build(s.Dialect())
	return s.scanUser(s.conn.QueryRow(ctx, statement, args...))
}

// GetUserByID looks an account up by primary key.
func (s *Store) GetUserByID(ctx context.Context, id int64) (User, error) {
	statement, args := query.From("users").Columns(splitColumns(userColumns)...).
		Where("id", "=", id).Build(s.Dialect())
	return s.scanUser(s.conn.QueryRow(ctx, statement, args...))
}

// GetUserByOIDC looks an account up by its identity provider identity.
func (s *Store) GetUserByOIDC(ctx context.Context, issuer, subject string) (User, error) {
	statement, args := query.From("users").Columns(splitColumns(userColumns)...).
		Where("oidc_issuer", "=", issuer).Where("oidc_subject", "=", subject).Build(s.Dialect())
	return s.scanUser(s.conn.QueryRow(ctx, statement, args...))
}

// PutSession stores (or refreshes) a console session.
func (s *Store) PutSession(ctx context.Context, session SessionData) error {
	statement, args := query.Into("sessions").
		Columns("sid", "user_id", "data", "expiry", "created_at").
		Values(session.SID, session.UserID, session.Data, dal.TimeArg(s.Dialect(), session.Expiry), dal.TimeArg(s.Dialect(), session.Created)).
		OnConflict([]string{"sid"}, []string{"data", "expiry"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: put session: %w", err)
	}
	return nil
}

// GetSession loads a session that has not expired yet.
func (s *Store) GetSession(ctx context.Context, sid string) (SessionData, error) {
	statement, args := query.From("sessions").
		Columns("sid", "user_id", "data", "expiry", "created_at").
		Where("sid", "=", sid).Build(s.Dialect())
	var (
		session SessionData
		expiry  any
		created any
	)
	err := s.conn.QueryRow(ctx, statement, args...).Scan(&session.SID, &session.UserID, &session.Data, &expiry, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionData{}, ErrNotFound
	}
	if err != nil {
		return SessionData{}, fmt.Errorf("bal: get session: %w", err)
	}
	if session.Expiry, err = dal.ParseTime(expiry); err != nil {
		return SessionData{}, err
	}
	if session.Created, err = dal.ParseTime(created); err != nil {
		return SessionData{}, err
	}
	if time.Now().After(session.Expiry) {
		_ = s.DeleteSession(ctx, sid)
		return SessionData{}, ErrNotFound
	}
	return session, nil
}

// DeleteSession removes a console session.
func (s *Store) DeleteSession(ctx context.Context, sid string) error {
	statement, args := query.DeleteFrom("sessions").Where("sid", "=", sid).Build(s.Dialect())
	_, err := s.conn.Exec(ctx, statement, args...)
	return err
}

// PurgeExpiredSessions drops sessions past their expiry.
func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	statement, args := query.DeleteFrom("sessions").
		Where("expiry", "<", dal.TimeArg(s.Dialect(), time.Now())).Build(s.Dialect())
	return s.conn.Exec(ctx, statement, args...)
}

// splitColumns turns a select list literal into identifiers.
func splitColumns(list string) []string {
	var out []string
	current := ""
	for _, r := range list {
		if r == ',' {
			out = append(out, trimSpace(current))
			current = ""
			continue
		}
		current += string(r)
	}
	if trimSpace(current) != "" {
		out = append(out, trimSpace(current))
	}
	return out
}

func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n') {
		end--
	}
	return value[start:end]
}

// DeleteUserSessions removes every console session of a user. Disabling an
// account has to end the logins that are already open, not just refuse new ones.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) (int64, error) {
	statement, args := query.DeleteFrom("sessions").Where("user_id", "=", userID).Build(s.Dialect())
	return s.conn.Exec(ctx, statement, args...)
}
