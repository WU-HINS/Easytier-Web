package db_test

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// freshDB is a database that starts out completely empty plus a raw
// handle on it, so a test can ask the engine itself what constraints exist.
type freshDB struct {
	store *bal.Store
	raw   *sql.DB
	live  func(query string, args ...any) (string, error)
}

func freshTarget(t *testing.T) freshDB {
	t.Helper()
	ctx := context.Background()
	base := os.Getenv("EASYTIER_TEST_PG_DSN")
	if base == "" {
		path := filepath.Join(t.TempDir(), "et.db")
		store, _, err := db.OpenWith(ctx, db.Config{
			Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
		})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("open raw sqlite: %v", err)
		}
		t.Cleanup(func() {
			_ = raw.Close()
			_ = store.Close()
		})
		return freshDB{store: store, raw: raw, live: func(query string, args ...any) (string, error) {
			var value string
			if err := raw.QueryRow(query, args...).Scan(&value); err != nil {
				return "", err
			}
			return value, nil
		}}
	}
	schema := fmt.Sprintf("single_%d", time.Now().UnixNano())
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	dsn := base + separator + "options=-c%20search_path%3D" + schema
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// Query through a handle bound to the private schema. Using the admin
	// connection would ask about public, where an older run may have left the
	// index behind and make the assertion vacuous.
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw postgres: %v", err)
	}
	// One cleanup in the right order: connections that use the schema must be
	// closed before the schema is dropped, otherwise the drop waits on them and
	// silently leaves the schema behind.
	t.Cleanup(func() {
		_ = store.Close()
		_ = raw.Close()
		_ = admin.Close()
		drop, err := sql.Open("pgx", base)
		if err != nil {
			return
		}
		defer drop.Close()
		if _, err := drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE"); err != nil {
			t.Logf("cleanup: drop schema %s: %v", schema, err)
		}
	})
	return freshDB{store: store, raw: raw, live: func(query string, args ...any) (string, error) {
		var value string
		if err := raw.QueryRow(query, args...).Scan(&value); err != nil {
			return "", err
		}
		return value, nil
	}}
}

// TestSingleAdminIndexExists checks the engine: the "at most one administrator"
// rule must be a constraint inside the database, not only a check in Go. The
// Go side used to run "SELECT ... WHERE NOT EXISTS" then insert, which let two
// concurrent setups on PostgreSQL both succeed because each transaction saw an
// empty table. A sequential test cannot see that, so this asserts the index.
func TestSingleAdminIndexExists(t *testing.T) {
	target := freshTarget(t)
	query := "SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'uq_users_single_admin'"
	if target.store.Dialect() == dal.Postgres {
		query = "SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'uq_users_single_admin'"
	}
	definition, err := target.live(query)
	if err != nil {
		t.Fatalf("the single admin index is missing: %v", err)
	}
	lower := strings.ToLower(definition)
	if !strings.Contains(lower, "unique") || !strings.Contains(lower, "role") {
		t.Fatalf("index is not a unique constraint on role: %s", definition)
	}
}

// TestSecondAdminIsRefused keeps the behaviour-level guarantee as well.
func TestSecondAdminIsRefused(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	t.Run(string(target.store.Dialect()), func(t *testing.T) {
		if _, err := target.store.CreateAdmin(ctx, "first-admin", "hash"); err != nil {
			t.Fatalf("first admin: %v", err)
		}
		if _, err := target.store.CreateAdmin(ctx, "second-admin", "hash"); err != bal.ErrAdminExists {
			t.Fatalf("second admin = %v, want ErrAdminExists", err)
		}
		for _, name := range []string{"alice", "bob", "carol"} {
			if _, err := target.store.CreateUser(ctx, name, "hash"); err != nil {
				t.Fatalf("create %s: %v", name, err)
			}
		}
		count, err := target.store.CountUsersByRole(ctx, "admin")
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Fatalf("admins = %d, want exactly 1", count)
		}
	})
}
