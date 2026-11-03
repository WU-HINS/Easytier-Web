package db_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestColdStartSurvivesAURIStyleSQLiteDSN covers the deploy half of F-223: the
// backup writer received the raw DSN, so a documented URI form (file:...?_pragma=)
// could be opened by the driver but not by the snapshot, and the very first start
// aborted (review team, r140).
func TestColdStartSurvivesAURIStyleSQLiteDSN(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "et.db")
	backups := filepath.Join(dir, "backups")
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: dsn, AutoAlign: true, Backup: true, BackupDir: backups,
	})
	if err != nil {
		t.Fatalf("cold start with a URI DSN: %v", err)
	}
	defer store.Close()
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	found := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "et-backup-") {
			found++
		}
	}
	if found == 0 {
		t.Fatal("no snapshot was written for the first schema change")
	}
}
