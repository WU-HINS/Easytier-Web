package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// newTestStore gives every end to end test a private database. By default that is
// a throwaway SQLite file; setting EASYTIER_E2E_PG to a PostgreSQL DSN runs the
// same scenarios against PostgreSQL in a schema of their own, so the reconcile
// and session paths are exercised on both dialects.
func newTestStore(t *testing.T, ctx context.Context) (*bal.Store, error) {
	t.Helper()
	base := os.Getenv("EASYTIER_E2E_PG")
	if base == "" {
		return openStore(db.Config{
			Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
		}, ctx)
	}
	schema := fmt.Sprintf("e2e_%d", time.Now().UnixNano())
	admin, err := sql.Open("pgx", base)
	if err != nil {
		return nil, err
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		_ = admin.Close()
		return nil, err
	}
	t.Cleanup(func() {
		_ = admin.Close()
		drop, err := sql.Open("pgx", base)
		if err != nil {
			return
		}
		defer drop.Close()
		_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	})
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return openStore(db.Config{
		Driver: dal.Postgres, DSN: base + separator + "options=-c%20search_path%3D" + schema,
		AutoAlign: true, Backup: false,
	}, ctx)
}

func openStore(config db.Config, ctx context.Context) (*bal.Store, error) {
	store, _, err := db.OpenWith(ctx, config)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// TestE2EStoreDialect keeps the PostgreSQL run honest: if the flag were ignored
// every scenario would still pass, just never on the other dialect.
func TestE2EStoreDialect(t *testing.T) {
	ctx := context.Background()
	store, err := newTestStore(t, ctx)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	want := dal.SQLite
	if os.Getenv("EASYTIER_E2E_PG") != "" {
		want = dal.Postgres
	}
	if got := store.Dialect(); got != want {
		t.Fatalf("e2e store dialect = %s, want %s", got, want)
	}
}
