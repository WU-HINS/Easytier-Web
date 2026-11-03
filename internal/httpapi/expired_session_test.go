package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestExpiredSessionIsRejected covers F-188: a session row whose expiry has passed
// must not authenticate, and the row should be dropped on the way out instead of
// lingering in the table.
func TestExpiredSessionIsRejected(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "root-owner")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	const stale = "expired-session-token-for-the-test"
	past := time.Now().Add(-2 * time.Hour)
	if err := h.store.PutSession(ctx, bal.SessionData{
		SID: stale, UserID: me.ID, Expiry: past, Created: past.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if status := replayCookie(t, h, stale); status != http.StatusUnauthorized {
		t.Fatalf("an expired session still authenticates: %d", status)
	}
	if _, err := h.store.GetSession(ctx, stale); err == nil {
		t.Fatal("the expired row was not purged")
	}
}
