package bal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

// User roles. The system has exactly one admin, created by the setup flow.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// ListUsers returns every account ordered by creation time.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	statement, args := query.From("users").Columns(splitColumns(userColumns)...).
		OrderBy("id", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		user, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

// CountUsersByRole counts accounts of one role.
func (s *Store) CountUsersByRole(ctx context.Context, role string) (int64, error) {
	statement := "SELECT COUNT(*) FROM " + query.Quote("users") +
		" WHERE " + query.Quote("role") + " = " + query.Placeholder(s.Dialect(), 1)
	var count int64
	if err := s.conn.QueryRow(ctx, statement, role).Scan(&count); err != nil {
		return 0, fmt.Errorf("bal: count users: %w", err)
	}
	return count, nil
}

// CountUsers counts every account.
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	statement := "SELECT COUNT(*) FROM " + query.Quote("users")
	var count int64
	if err := s.conn.QueryRow(ctx, statement).Scan(&count); err != nil {
		return 0, fmt.Errorf("bal: count users: %w", err)
	}
	return count, nil
}

// ErrAdminExists is returned when the single administrator is already present.
var ErrAdminExists = errors.New("bal: an administrator already exists")

// CreateAdmin inserts the single administrator in one statement: the INSERT is
// guarded by "no admin exists yet", so two concurrent setup requests cannot
// both succeed. The guard and the write are the same statement, which is the
// only race-free option without an explicit transaction.
func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (User, error) {
	now := time.Now()
	statement := "INSERT INTO " + query.Quote("users") +
		" (" + query.Join("username", "password", "role", "created_at") + ")" +
		" SELECT " + query.Placeholder(s.Dialect(), 1) + ", " + query.Placeholder(s.Dialect(), 2) +
		", " + query.Placeholder(s.Dialect(), 3) + ", " + query.Placeholder(s.Dialect(), 4) +
		" WHERE NOT EXISTS (SELECT 1 FROM " + query.Quote("users") +
		" WHERE " + query.Quote("role") + " = " + query.Placeholder(s.Dialect(), 5) + ")" +
		" RETURNING id"
	var id int64
	err := s.conn.QueryRow(ctx, statement, username, passwordHash, RoleAdmin,
		dal.TimeArg(s.Dialect(), now), RoleAdmin).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrAdminExists
	}
	// Two concurrent setups can both pass the WHERE NOT EXISTS check before
	// either commits; the partial unique index then rejects the loser. That still
	// means an admin exists, so the caller must see 409 rather than a driver error.
	if isUniqueViolation(err) {
		return User{}, ErrAdminExists
	}
	if err != nil {
		return User{}, fmt.Errorf("bal: create admin: %w", err)
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash, Role: RoleAdmin, CreatedAt: now}, nil
}

func (s *Store) createUserWithRole(ctx context.Context, username, passwordHash, role string) (User, error) {
	now := time.Now()
	statement, args := query.Into("users").
		Columns("username", "password", "role", "created_at").
		Values(username, passwordHash, role, dal.TimeArg(s.Dialect(), now)).
		Build(s.Dialect())
	statement += " RETURNING id"
	var id int64
	if err := s.conn.QueryRow(ctx, statement, args...).Scan(&id); err != nil {
		return User{}, fmt.Errorf("bal: create user: %w", err)
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash, Role: role, CreatedAt: now}, nil
}

// SetUserDisabled flips the disabled flag.
func (s *Store) SetUserDisabled(ctx context.Context, userID int64, disabled bool) error {
	statement, args := query.Table("users").
		Set("disabled", dal.BoolArg(s.Dialect(), disabled)).
		Where("id", "=", userID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: disable user: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes an account. Sessions and memberships cascade.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	statement, args := query.DeleteFrom("users").Where("id", "=", userID).Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: delete user: %w", err)
	}
	return nil
}

// ErrOIDCIdentityTaken means another account already holds this IdP identity.
var ErrOIDCIdentityTaken = errors.New("bal: this identity is already bound to another account")

// SetUserOIDC binds an identity to an account.
func (s *Store) SetUserOIDC(ctx context.Context, userID int64, issuer, subject string) error {
	statement, args := query.Table("users").
		Set("oidc_issuer", issuer).Set("oidc_subject", subject).
		Where("id", "=", userID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		if isUniqueViolation(err) {
			// Two accounts claiming the same identity is a conflict, not a server
			// fault (review team, r134).
			return ErrOIDCIdentityTaken
		}
		return fmt.Errorf("bal: bind oidc: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearUserOIDC removes the binding of an account.
func (s *Store) ClearUserOIDC(ctx context.Context, userID int64) error {
	statement := "UPDATE " + query.Quote("users") +
		" SET " + query.Quote("oidc_issuer") + " = NULL, " + query.Quote("oidc_subject") + " = NULL" +
		" WHERE " + query.Quote("id") + " = " + query.Placeholder(s.Dialect(), 1)
	if _, err := s.conn.Exec(ctx, statement, userID); err != nil {
		return fmt.Errorf("bal: unbind oidc: %w", err)
	}
	return nil
}

// ExistsByUsername reports whether a username is taken.
func (s *Store) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	statement, args := query.From("users").Columns("id").
		Where("username", "=", username).Build(s.Dialect())
	var id int64
	err := s.conn.QueryRow(ctx, statement, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// SetPasswordHash replaces a user password hash. Self service rotation needs it;
// the admin surface deliberately has no way to reach it.
func (s *Store) SetPasswordHash(ctx context.Context, userID int64, hash string) error {
	statement, args := query.Table("users").
		Set("password", hash).
		Where("id", "=", userID).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: set password: %w", err)
	}
	return nil
}
