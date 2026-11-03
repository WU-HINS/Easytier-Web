package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestPurgeExpiredSessionsOnlyDropsTheDead covers F-191: the housekeeping query
// deletes rows by comparing a timestamp, which is exactly the shape that breaks
// on one dialect and not the other. Deleting too much would log live users out;
// deleting too little grows the table for ever.
func TestPurgeExpiredSessionsOnlyDropsTheDead(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	user, err := store.CreateUser(ctx, "purge-user", "hash")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now()
	live := bal.SessionData{SID: "live-session", UserID: user.ID, Expiry: now.Add(time.Hour), Created: now}
	dead := bal.SessionData{SID: "dead-session", UserID: user.ID, Expiry: now.Add(-time.Hour), Created: now.Add(-2 * time.Hour)}
	for _, session := range []bal.SessionData{live, dead} {
		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("put %s: %v", session.SID, err)
		}
	}

	removed, err := store.PurgeExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("purged %d rows, want exactly the expired one", removed)
	}
	if _, err := store.GetSession(ctx, live.SID); err != nil {
		t.Fatalf("the live session was purged: %v", err)
	}
	if _, err := store.GetSession(ctx, dead.SID); err == nil {
		t.Fatal("the expired session survived the purge")
	}
}
