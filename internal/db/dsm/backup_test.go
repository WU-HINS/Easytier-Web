package dsm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/easytier/easytier-console/internal/db/dal"
)

type dialectOnly struct{ dal.Conn }

func (dialectOnly) Dialect() dal.Dialect { return dal.SQLite }

// TestBackupCreatesMissingDirectory covers F-214: the shipped Docker, compose and
// .env files point EASYTIER_DB_BACKUP_DIR at a directory nobody creates, so the
// first start aborted at the first schema change.
func TestBackupCreatesMissingDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "et.db")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(root, "nested", "deeper", "backups")
	path, err := Backup(context.Background(), dialectOnly{}, BackupOptions{
		Enabled: true, Dir: target, SQLitePath: source,
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("backup content = %q", data)
	}
}
