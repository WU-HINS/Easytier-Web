package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
)

func pgDSN() string { return os.Getenv("EASYTIER_TEST_PG_DSN") }

// uniqueName keeps repeated runs against a persistent PostgreSQL database from
// colliding on the username unique index.
func uniqueName(prefix string, store *bal.Store) string {
	return fmt.Sprintf("%s-%s-%d", prefix, store.Dialect(), time.Now().UnixNano())
}

func openDialects(t *testing.T) []*bal.Store {
	t.Helper()
	ctx := context.Background()
	sqliteStore, _, err := db.OpenWith(ctx, db.Config{
		Driver:    dal.SQLite,
		DSN:       filepath.Join(t.TempDir(), "et.db"),
		AutoAlign: true,
		Backup:    false,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	stores := []*bal.Store{sqliteStore}
	if dsn := pgDSN(); dsn != "" {
		pgStore, _, err := db.OpenWith(ctx, db.Config{
			Driver:    dal.Postgres,
			DSN:       dsn,
			AutoAlign: true,
			Backup:    false,
		})
		if err != nil {
			t.Fatalf("open postgres: %v", err)
		}
		t.Cleanup(func() { _ = pgStore.Close() })
		stores = append(stores, pgStore)
	}
	return stores
}

// TestAlignmentIsIdempotent proves declarative alignment is a fixed point: the
// second open against the same database performs no DDL.
func TestAlignmentIsIdempotent(t *testing.T) {
	ctx := context.Background()
	sqlitePath := filepath.Join(t.TempDir(), "idempotent.db")
	first, report, err := db.OpenWith(ctx, db.Config{Driver: dal.SQLite, DSN: sqlitePath, AutoAlign: true, Backup: false})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if len(report.Changes) == 0 {
		t.Fatalf("first open should create the schema")
	}
	_ = first.Close()

	second, report, err := db.OpenWith(ctx, db.Config{Driver: dal.SQLite, DSN: sqlitePath, AutoAlign: true, Backup: false})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	_ = second.Close()
	for _, change := range report.Changes {
		if change.SQL != "" {
			t.Fatalf("second open applied DDL: %s", change)
		}
	}

	if dsn := pgDSN(); dsn != "" {
		pgFirst, _, err := db.OpenWith(ctx, db.Config{Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false})
		if err != nil {
			t.Fatalf("postgres first open: %v", err)
		}
		_ = pgFirst.Close()
		pgSecond, pgReport, err := db.OpenWith(ctx, db.Config{Driver: dal.Postgres, DSN: dsn, AutoAlign: true, Backup: false})
		if err != nil {
			t.Fatalf("postgres second open: %v", err)
		}
		_ = pgSecond.Close()
		for _, change := range pgReport.Changes {
			if change.SQL != "" {
				t.Fatalf("postgres second open applied DDL: %s", change)
			}
		}
	}
}

func TestDeviceRegistryRoundTrip(t *testing.T) {
	for _, store := range openDialects(t) {
		t.Run(string(store.Dialect()), func(t *testing.T) {
			ctx := context.Background()
			user, err := store.CreateUser(ctx, uniqueName("dev", store), "hash")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			first := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
			device := bal.Device{
				UserID: user.ID, MachineID: "11111111-1111-1111-1111-111111111111",
				Hostname: "laptop", EasytierVersion: "2.7.0", DeviceOS: "{}",
				ClientURL: "tcp://10.0.0.2:1", FirstSeen: first, LastSeen: first,
			}
			if err := store.UpsertDevice(ctx, device); err != nil {
				t.Fatalf("upsert: %v", err)
			}
			if err := store.SetDeviceAlias(ctx, user.ID, device.MachineID, "primary"); err != nil {
				t.Fatalf("alias: %v", err)
			}
			later := time.Now().UTC().Truncate(time.Second)
			device.Hostname = "laptop-renamed"
			device.LastSeen = later
			device.Alias = "ignored"
			if err := store.UpsertDevice(ctx, device); err != nil {
				t.Fatalf("second upsert: %v", err)
			}
			stored, err := store.GetDevice(ctx, user.ID, device.MachineID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if stored.Alias != "primary" || stored.Hostname != "laptop-renamed" {
				t.Fatalf("stored = %+v", stored)
			}
			if !stored.FirstSeen.Equal(first) {
				t.Fatalf("first_seen = %v, want %v", stored.FirstSeen, first)
			}
			if !stored.LastSeen.Equal(later) {
				t.Fatalf("last_seen = %v, want %v", stored.LastSeen, later)
			}
		})
	}
}

func TestNetworkIntentAndRevisionCAS(t *testing.T) {
	for _, store := range openDialects(t) {
		t.Run(string(store.Dialect()), func(t *testing.T) {
			ctx := context.Background()
			user, err := store.CreateUser(ctx, uniqueName("net", store), "hash")
			if err != nil {
				t.Fatalf("create user: %v", err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			cidr := "10.42.0.0/24"
			network := bal.Network{
				UserID: user.ID, ID: "net-1", DisplayName: "lab", NetworkName: "lab-net",
				NetworkSecret: "s3cret", NetworkingMethod: "Manual",
				PeerURLs: []string{"tcp://1.2.3.4:11010"}, VirtualCIDR: &cidr,
				SecureMode: true, CreateTime: now, UpdateTime: now,
			}
			if err := store.CreateNetwork(ctx, network); err != nil {
				t.Fatalf("create network: %v", err)
			}
			loaded, err := store.GetNetwork(ctx, user.ID, network.ID)
			if err != nil {
				t.Fatalf("get network: %v", err)
			}
			if !loaded.SecureMode || len(loaded.PeerURLs) != 1 {
				t.Fatalf("loaded = %+v", loaded)
			}

			machine := "22222222-2222-2222-2222-222222222222"
			if err := store.UpsertDevice(ctx, bal.Device{
				UserID: user.ID, MachineID: machine, Hostname: "node-1", FirstSeen: now, LastSeen: now,
			}); err != nil {
				t.Fatalf("device: %v", err)
			}
			if err := store.AddMember(ctx, bal.NetworkMember{
				UserID: user.ID, NetworkID: network.ID, ID: "member-1", DeviceID: machine,
				CreateTime: now, UpdateTime: now,
			}); err != nil {
				t.Fatalf("add member: %v", err)
			}
			members, err := store.ListMembers(ctx, user.ID, network.ID)
			if err != nil {
				t.Fatalf("list members: %v", err)
			}
			if len(members) != 1 {
				t.Fatalf("members = %d", len(members))
			}

			if err := store.SetManagedConfigRevision(ctx, user.ID, machine, "rev-1", nil); err != nil {
				t.Fatalf("set revision: %v", err)
			}
			expected := "rev-1"
			if err := store.SetManagedConfigRevision(ctx, user.ID, machine, "rev-2", &expected); err != nil {
				t.Fatalf("cas revision: %v", err)
			}
			stale := "rev-1"
			if err := store.SetManagedConfigRevision(ctx, user.ID, machine, "rev-3", &stale); err != bal.ErrRevisionConflict {
				t.Fatalf("stale cas = %v, want ErrRevisionConflict", err)
			}
		})
	}
}
