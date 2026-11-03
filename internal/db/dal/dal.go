// Package dal is the data abstraction layer (HiDNS DAL): it owns the SQL
// dialect, the query compiler and the connection abstraction, and knows nothing
// about any business entity.
package dal

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Dialect is the SQL flavour.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// ErrNotFound is returned by repositories when a lookup matches no row.
var ErrNotFound = errors.New("db: not found")

// Conn is the only database surface repositories may touch.
type Conn interface {
	Dialect() Dialect
	Exec(ctx context.Context, query string, args ...any) (int64, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Begin(ctx context.Context) (Tx, error)
	Ping(ctx context.Context) error
	Close() error
}

// Rows iterates a result set.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// Row reads a single row.
type Row interface {
	Scan(dest ...any) error
}

// Tx is a transaction with the same call shape as Conn.
type Tx interface {
	Exec(ctx context.Context, query string, args ...any) (int64, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Commit() error
	Rollback() error
}

// TimeArg renders a timestamp for a dialect.
func TimeArg(d Dialect, t time.Time) any {
	if d == Postgres {
		return t.UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// ParseTime accepts whatever the driver hands back for a timestamp column.
func ParseTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return v, nil
	case string:
		return time.Parse(time.RFC3339Nano, v)
	case []byte:
		return time.Parse(time.RFC3339Nano, string(v))
	default:
		return time.Time{}, errors.New("db: unsupported timestamp column")
	}
}

// BoolArg renders a boolean for a dialect.
func BoolArg(d Dialect, b bool) any {
	if d == Postgres {
		return b
	}
	if b {
		return int64(1)
	}
	return int64(0)
}

// ParseBool accepts the boolean encodings of both drivers.
func ParseBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case []byte:
		return string(v) == "1" || string(v) == "true" || string(v) == "t"
	case string:
		return v == "1" || v == "true" || v == "t"
	default:
		return false
	}
}

// NullString keeps nullable text columns ergonomic.
type NullString struct {
	String string
	Valid  bool
}

// Ptr returns a pointer to the value when valid.
func (n NullString) Ptr() *string {
	if !n.Valid {
		return nil
	}
	value := n.String
	return &value
}

// StringPtr boxes an optional string for scanning.
func StringPtr(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
