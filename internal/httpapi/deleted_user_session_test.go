package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
)

// TestSessionOfDeletedUserIsRejected covers the property F-101 claimed in r48 but
// which had no test in the tree: after the administrator deletes an account, the
// cookie that account was holding must stop working (401, not 200 and not 500).
func TestSessionOfDeletedUserIsRejected(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "gus", strongPassword)
	cookie := sessionCookieValue(t, h, user)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "gus")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if status, body := h.do(t, admin, http.MethodDelete, "/api/v1/admin/users/"+strconv.FormatInt(me.ID, 10), nil); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("delete = %d %s", status, body)
	}

	if status := replayCookie(t, h, cookie); status != http.StatusUnauthorized {
		t.Fatalf("a deleted account still reads data: %d", status)
	}
}
