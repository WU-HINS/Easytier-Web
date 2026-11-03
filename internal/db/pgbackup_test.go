package db_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestPostgresBackupCommand covers the PostgreSQL snapshot path: it shells out to
// an operator supplied dump command. Without a command the console keeps going
// (there is no way to snapshot a remote database), with one it must run before
// any DDL, and a failing command must abort the alignment.
func TestPostgresBackupCommand(t *testing.T) {
	dsn := pgDSN()
	if dsn == "" {
		t.Skip("EASYTIER_TEST_PG_DSN not set")
	}
	ctx := context.Background()

	// Start from a known schema.
	if _, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false,
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	drop := func() {
		raw, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("raw open: %v", err)
		}
		if _, err := raw.Exec("DROP TABLE IF EXISTS console_instances"); err != nil {
			t.Fatalf("drop: %v", err)
		}
		_ = raw.Close()
	}

	// 1. No command: alignment proceeds and reports no snapshot.
	drop()
	store, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: true, BackupDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open without a dump command: %v", err)
	}
	if report.BackupPath != "" {
		t.Fatalf("a snapshot path was reported without a command: %s", report.BackupPath)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// 2. With a command: the file is written before the DDL runs.
	drop()
	dir := t.TempDir()
	store, report, err = db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: true,
		BackupDir: dir, BackupCmd: "echo pg-dump-ok",
	})
	if err != nil {
		t.Fatalf("open with a dump command: %v", err)
	}
	if report.BackupPath == "" {
		t.Fatal("no snapshot path was reported")
	}
	payload, err := os.ReadFile(report.BackupPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !strings.Contains(string(payload), "pg-dump-ok") {
		t.Fatalf("snapshot does not contain the command output: %q", string(payload))
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// 3. A failing command aborts the alignment and leaves the schema alone.
	drop()
	if _, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: true,
		BackupDir: t.TempDir(), BackupCmd: "false",
	}); err == nil {
		t.Fatal("alignment proceeded even though the dump command failed")
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer raw.Close()
	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'console_instances'").
		Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatal("the table was created even though the snapshot failed")
	}
	_ = filepath.Join
}
