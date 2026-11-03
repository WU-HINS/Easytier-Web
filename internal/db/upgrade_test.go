package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestMissingTableIsRecreatedWithoutDataLoss covers the upgrade path that runs
// on every start: a database written by an older build lacks objects a newer
// build needs. Alignment must add them and leave the data alone.
func TestMissingTableIsRecreatedWithoutDataLoss(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "et.db")

	first, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	fingerprint := report.Fingerprint
	hash, err := auth.Hash("Sup3r-Secret-Pass!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := first.CreateUser(ctx, "upgrader", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	cidr := "10.61.0.0/29"
	now := time.Now().UTC()
	if err := first.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-upgrade", DisplayName: "upgrade", NetworkName: "upgrade-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
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

	second, report2, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	recreated := false
	for _, change := range report2.Changes {
		if change.Action == "create_table" && change.Table == "console_instances" {
			recreated = true
		}
	}
	if !recreated {
		t.Fatalf("the missing table was not recreated: %+v", report2.Changes)
	}
	if report2.Fingerprint != fingerprint {
		t.Fatalf("fingerprint changed: %s then %s", fingerprint, report2.Fingerprint)
	}
	if _, err := second.GetUserByName(ctx, "upgrader"); err != nil {
		t.Fatalf("user lost: %v", err)
	}
	networks, err := second.ListNetworks(ctx, user.ID)
	if err != nil || len(networks) != 1 {
		t.Fatalf("networks after upgrade = %d, %v", len(networks), err)
	}
	if networks[0].NetworkName != "upgrade-net" {
		t.Fatalf("network name changed: %q", networks[0].NetworkName)
	}
	if err := second.TouchInstance(ctx, "after-upgrade", "test"); err != nil {
		t.Fatalf("the recreated table is unusable: %v", err)
	}
}

// TestMissingTableIsRecreatedOnPostgres is the same upgrade path on the dialect
// where DDL ordering actually matters (r8: a fresh bootstrap failed because a
// child table was created before its parent).
func TestMissingTableIsRecreatedOnPostgres(t *testing.T) {
	dsn := pgDSN()
	if dsn == "" {
		t.Skip("EASYTIER_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A user row that must survive the round trip.
	hash, err := auth.Hash("Sup3r-Secret-Pass!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	name := "pg-upgrader-" + time.Now().Format("150405.000000")
	if _, err := store.CreateUser(ctx, name, hash); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec("DROP TABLE IF EXISTS console_instances"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("raw close: %v", err)
	}

	reopened, report, err := db.OpenWith(ctx, db.Config{
		Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	recreated := false
	for _, change := range report.Changes {
		if change.Action == "create_table" && change.Table == "console_instances" {
			recreated = true
		}
	}
	if !recreated {
		t.Fatalf("postgres did not recreate the table: %+v", report.Changes)
	}
	if _, err := reopened.GetUserByName(ctx, name); err != nil {
		t.Fatalf("user lost during the postgres upgrade: %v", err)
	}
	if err := reopened.TouchInstance(ctx, "pg-after-upgrade", "test"); err != nil {
		t.Fatalf("recreated table unusable: %v", err)
	}
}
