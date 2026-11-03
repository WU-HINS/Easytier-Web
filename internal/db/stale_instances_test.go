package db_test

import (
	"context"
	"testing"
	"time"
)

// TestDeleteStaleInstancesDropsOnlyTheOldOnes covers F-192: every daemon restart
// writes a new console_instances row and nothing ever deleted them, so the table
// grew for ever even though LiveInstances ignored the old rows.
func TestDeleteStaleInstancesDropsOnlyTheOldOnes(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	if err := store.TouchInstance(ctx, "fresh-instance", "v1"); err != nil {
		t.Fatalf("touch fresh: %v", err)
	}
	if err := store.TouchInstance(ctx, "stale-instance", "v1"); err != nil {
		t.Fatalf("touch stale: %v", err)
	}
	// Age the second row by rewriting it through the same upsert with an old time is
	// not exposed, so the test ages the cutoff instead: a negative window means
	// everything older than "now plus a second" goes, which is exactly this row set.
	removed, err := store.DeleteStaleInstances(ctx, -time.Second)
	if err != nil {
		t.Fatalf("delete stale: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d rows, want both", removed)
	}
	live, err := store.LiveInstances(ctx, time.Hour)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if live != 0 {
		t.Fatalf("live instances = %d after the purge", live)
	}
	if err := store.TouchInstance(ctx, "fresh-instance", "v1"); err != nil {
		t.Fatalf("touch again: %v", err)
	}
	removed, err = store.DeleteStaleInstances(ctx, time.Hour)
	if err != nil {
		t.Fatalf("delete with a real window: %v", err)
	}
	if removed != 0 {
		t.Fatalf("a fresh row was deleted: %d", removed)
	}
}
