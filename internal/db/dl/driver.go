// Package dl is the driver layer (HiDNS DL): it owns connection pools and the
// concrete SQL driver. It is the only package that imports database/sql for
// runtime traffic, and business code must never import it.
package dl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// Config is one driver configuration.
type Config struct {
	DSN      string
	MaxOpen  int
	MaxIdle  int
	ConnLife time.Duration
}

// baseDriver holds the logic every dialect shares; concrete drivers only
// provide the sql driver name, the DSN shape and the dialect tag.
type baseDriver struct {
	name    string
	dialect dal.Dialect
}

func (b baseDriver) open(ctx context.Context, config Config) (dal.Conn, error) {
	handle, err := sql.Open(b.name, config.DSN)
	if err != nil {
		return nil, fmt.Errorf("dl: open %s: %w", b.dialect, err)
	}
	if config.MaxOpen > 0 {
		handle.SetMaxOpenConns(config.MaxOpen)
	}
	if config.MaxIdle > 0 {
		handle.SetMaxIdleConns(config.MaxIdle)
	}
	if config.ConnLife > 0 {
		handle.SetConnMaxLifetime(config.ConnLife)
	}
	if err := handle.PingContext(ctx); err != nil {
		handle.Close()
		return nil, fmt.Errorf("dl: ping %s: %w", b.dialect, err)
	}
	return &sqlConn{db: handle, dialect: b.dialect}, nil
}

// sqlConn adapts *sql.DB to dal.Conn.
type sqlConn struct {
	db      *sql.DB
	dialect dal.Dialect
}

func (c *sqlConn) Dialect() dal.Dialect           { return c.dialect }
func (c *sqlConn) Ping(ctx context.Context) error { return c.db.PingContext(ctx) }
func (c *sqlConn) Close() error                   { return c.db.Close() }

func (c *sqlConn) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := c.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return rows, nil
}

func (c *sqlConn) Query(ctx context.Context, query string, args ...any) (dal.Rows, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *sqlConn) QueryRow(ctx context.Context, query string, args ...any) dal.Row {
	return c.db.QueryRowContext(ctx, query, args...)
}

func (c *sqlConn) Begin(ctx context.Context) (dal.Tx, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqlTx{tx: tx}, nil
}

type sqlTx struct{ tx *sql.Tx }

func (t *sqlTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return rows, nil
}

func (t *sqlTx) QueryRow(ctx context.Context, query string, args ...any) dal.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *sqlTx) Commit() error   { return t.tx.Commit() }
func (t *sqlTx) Rollback() error { return t.tx.Rollback() }

// NormalizeDSN applies per-dialect connection pragmas and validation.
func NormalizeDSN(d dal.Dialect, dsn string) (string, error) {
	switch d {
	case dal.SQLite:
		// A DSN that already carries options used to be returned untouched, which
		// silently dropped foreign_keys, WAL and the busy timeout. Merge instead of
		// abandoning the defaults (review team, r140).
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		missing := make([]string, 0, 3)
		for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)"} {
			key := strings.SplitN(pragma, "(", 2)[0]
			if !strings.Contains(dsn, "_pragma="+key+"(") {
				missing = append(missing, "_pragma="+pragma)
			}
		}
		if len(missing) == 0 {
			return dsn, nil
		}
		return dsn + separator + strings.Join(missing, "&"), nil
	case dal.Postgres:
		if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
			return "", fmt.Errorf("dl: postgres DSN must start with postgres:// or postgresql://")
		}
		return dsn, nil
	default:
		return "", fmt.Errorf("dl: unsupported dialect %q", d)
	}
}
