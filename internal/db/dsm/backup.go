package dsm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// BackupOptions controls the pre-reconcile snapshot.
type BackupOptions struct {
	// Enabled turns the snapshot on.
	Enabled bool
	// SQLitePath is the database file; empty means the dialect is not file based.
	SQLitePath string
	// Dir is where snapshots are written.
	Dir string
	// Command is an optional operator-supplied dump command (for example
	// "pg_dump --schema-only"). It runs with the DSN in EASYTIER_DB_DSN.
	Command string
}

// Backup writes a snapshot before the schema changes. It returns the path it
// created, or an empty string when no snapshot applies.
func Backup(ctx context.Context, conn dal.Conn, options BackupOptions) (string, error) {
	if !options.Enabled {
		return "", nil
	}
	dir := options.Dir
	if dir == "" {
		dir = "."
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	// The shipped Docker/compose configurations point at a directory that does
	// not exist yet, so a first start used to abort at the very first schema
	// change. Creating it is the difference between "works out of the box" and
	// "the documented command cannot succeed" (review team, r135).
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("dsm: create backup directory %s: %w", dir, err)
	}
	switch conn.Dialect() {
	case dal.SQLite:
		if options.SQLitePath == "" {
			return "", nil
		}
		target := filepath.Join(dir, fmt.Sprintf("et-backup-%s.db", stamp))
		if err := copyFile(options.SQLitePath, target); err != nil {
			return "", fmt.Errorf("dsm: sqlite backup: %w", err)
		}
		return target, nil
	case dal.Postgres:
		if strings.TrimSpace(options.Command) == "" {
			// Without an operator supplied dump command we cannot snapshot a
			// remote database; reconciliation still runs, but the caller is told.
			return "", nil
		}
		target := filepath.Join(dir, fmt.Sprintf("et-backup-%s.sql", stamp))
		output, err := os.Create(target)
		if err != nil {
			return "", fmt.Errorf("dsm: create backup file: %w", err)
		}
		defer output.Close()
		command := exec.CommandContext(ctx, "sh", "-c", options.Command)
		command.Stdout = output
		if err := command.Run(); err != nil {
			return "", fmt.Errorf("dsm: backup command: %w", err)
		}
		return target, nil
	default:
		return "", nil
	}
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL plus a nanosecond stamp: two backups inside the same second must
	// not overwrite each other.
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		// A half written snapshot must not look like a usable backup.
		_ = os.Remove(target)
		return err
	}
	return out.Sync()
}
