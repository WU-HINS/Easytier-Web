package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestFreshPostgresBootstrap guards F-023: on an empty PostgreSQL database the
// DDL must run in foreign key dependency order. SQLite tolerates a table that
// references a not-yet-created parent; PostgreSQL does not, which is why this
// case has to run against a database that has never seen the schema.
func TestFreshPostgresBootstrap(t *testing.T) {
	dsn := os.Getenv("EASYTIER_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("EASYTIER_TEST_PG_DSN not set")
	}
	adminDSN, err := replaceDatabase(dsn, "postgres")
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}

	name := fmt.Sprintf("et_fresh_%d", time.Now().UnixNano()%1_000_000)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Skipf("cannot create database (insufficient privileges): %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})

	freshDSN, err := replaceDatabase(dsn, name)
	if err != nil {
		t.Fatalf("build dsn: %v", err)
	}

	ctx := context.Background()
	store, report, err := OpenWith(ctx, Config{
		Driver:    dal.Postgres,
		DSN:       freshDSN,
		AutoAlign: true,
		Backup:    false,
	})
	if err != nil {
		t.Fatalf("fresh bootstrap failed: %v", err)
	}
	if len(report.Changes) == 0 {
		t.Fatalf("expected the fresh database to need DDL")
	}
	created := 0
	for _, change := range report.Changes {
		if change.Action == "create_table" {
			created++
		}
	}
	if created < 8 {
		t.Fatalf("created %d tables, expected the full schema", created)
	}
	_ = store.Close()

	// Second open must be a no-op: alignment is a fixed point.
	again, secondReport, err := OpenWith(ctx, Config{
		Driver: dal.Postgres, DSN: freshDSN, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = again.Close() }()
	for _, change := range secondReport.Changes {
		if change.SQL != "" {
			t.Fatalf("second open applied DDL: %s", change)
		}
	}
}

func replaceDatabase(dsn, database string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("not a postgres dsn")
	}
	parsed.Path = "/" + strings.TrimPrefix(database, "/")
	return parsed.String(), nil
}
