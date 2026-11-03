package db_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestDryRunReportsWithoutApplying covers F-108: an operator uses --dry-run to
// see what an upgrade would do. If it applied anything the "check only" command
// would mutate production.
func TestDryRunReportsWithoutApplying(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "et.db")

	first, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE console_instances"); err != nil {
		t.Fatalf("drop: %v", err)
	}

	_, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, DryRun: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	listed := false
	for _, change := range report.Changes {
		if change.Action == "create_table" && change.Table == "console_instances" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("dry run did not report the pending change: %+v", report.Changes)
	}

	// Nothing may have been applied.
	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='console_instances'").
		Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatal("dry run applied DDL")
	}
	_ = raw.Close()
}

// TestLegacyDatabaseIsOnlyReported covers the conservative rule: when a
// pre-rewrite database is detected the run reports it instead of pretending the
// schema is ours.
func TestLegacyDatabaseIsOnlyReported(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE \"groups\" (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO \"groups\" (id, name) VALUES (1, 'old')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	if !report.Legacy {
		t.Fatalf("a pre-rewrite table was not reported as legacy")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file disappeared: %v", err)
	}

	// The legacy row is untouched.
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw reopen: %v", err)
	}
	defer raw.Close()
	var name string
	if err := raw.QueryRow("SELECT name FROM \"groups\" WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("legacy row lost: %v", err)
	}
	if name != "old" {
		t.Fatalf("legacy row changed: %q", name)
	}
}

// TestEnvConfigValidation covers the only configuration surface that cannot be
// validated at compile time.
func TestEnvConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		driver string
		dsn    string
		ok     bool
	}{
		{"missing driver", "", "/tmp/x.db", false},
		{"unknown driver", "mysql", "/tmp/x.db", false},
		{"missing dsn", "sqlite", "", false},
		{"sqlite alias", "sqlite3", "/tmp/x.db", true},
		{"postgres alias", "pg", "postgres://x", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("EASYTIER_DB_DRIVER", testCase.driver)
			t.Setenv("EASYTIER_DB_DSN", testCase.dsn)
			_, err := db.EnvConfig()
			if testCase.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !testCase.ok && err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}
