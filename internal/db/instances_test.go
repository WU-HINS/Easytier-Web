package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
)

// TestInstanceHeartbeatDetectsReplicas covers F-028: two console processes
// sharing a database must be able to notice each other, because device tunnels
// are per process and a second replica silently sees devices as offline.
func TestInstanceHeartbeatDetectsReplicas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "et.db")

	open := func() *balStoreHandle {
		store, _, err := db.OpenWith(ctx, db.Config{
			Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
		})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return &balStoreHandle{store: store}
	}

	first := open()
	second := open()

	if err := first.store.TouchInstance(ctx, "instance-a", "test"); err != nil {
		t.Fatalf("touch a: %v", err)
	}
	if count, err := first.store.LiveInstances(ctx, time.Minute); err != nil || count != 1 {
		t.Fatalf("live after first = %d, %v; want 1", count, err)
	}

	if err := second.store.TouchInstance(ctx, "instance-b", "test"); err != nil {
		t.Fatalf("touch b: %v", err)
	}
	for name, handle := range map[string]*balStoreHandle{"first": first, "second": second} {
		count, err := handle.store.LiveInstances(ctx, time.Minute)
		if err != nil {
			t.Fatalf("%s count: %v", name, err)
		}
		if count != 2 {
			t.Fatalf("%s live instances = %d, want 2 (shared database)", name, count)
		}
	}

	// Re-touching an instance updates the row instead of adding one.
	if err := first.store.TouchInstance(ctx, "instance-a", "test"); err != nil {
		t.Fatalf("re-touch: %v", err)
	}
	if count, _ := first.store.LiveInstances(ctx, time.Minute); count != 2 {
		t.Fatalf("live after re-touch = %d, want 2", count)
	}

	// A window shorter than the gap excludes nothing here, but a zero-length
	// window must not report live instances.
	if count, _ := first.store.LiveInstances(ctx, -time.Second); count != 0 {
		t.Fatalf("live with negative window = %d, want 0", count)
	}
}

type balStoreHandle struct {
	store *bal.Store
}
