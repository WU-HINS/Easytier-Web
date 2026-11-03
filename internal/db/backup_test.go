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

// TestBackupRunsBeforeSchemaChange covers F-106: a snapshot must be written
// before any DDL, and a failing snapshot must abort the alignment instead of
// half-applying it.
func TestBackupRunsBeforeSchemaChange(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "et.db")

	// Build a database, then remove a table so the next start has work to do.
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
	if err := raw.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	// 1. A healthy backup directory: the snapshot exists afterwards.
	goodDir := t.TempDir()
	store, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: true, BackupDir: goodDir,
	})
	if err != nil {
		t.Fatalf("open with backup: %v", err)
	}
	if report.BackupPath == "" {
		t.Fatalf("no snapshot path was reported")
	}
	if _, err := os.Stat(report.BackupPath); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// 2. A broken backup directory: the alignment must fail and leave the schema
	// alone (the table stays missing, evidence that no DDL ran).
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE console_instances"); err != nil {
		t.Fatalf("drop again: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	// A missing directory is no longer an error: the backup writer creates it
	// (F-214). Make the directory impossible by putting a regular file in the way,
	// which is the remaining honest way to make the snapshot fail.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	brokenDir := filepath.Join(blocker, "backups")
	if _, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: true, BackupDir: brokenDir,
	}); err == nil {
		t.Fatal("alignment proceeded even though the snapshot failed")
	}

	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer raw.Close()
	var count int
	if err := raw.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='console_instances'").
		Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatal("the table was created even though the snapshot failed")
	}
}
