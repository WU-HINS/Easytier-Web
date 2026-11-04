package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestSessionListingIsScopedAndAdminOnly covers the two session routes the
// original console exposed: a user sees their own (empty) list, and the internal
// listing stays admin only.
func TestSessionListingIsScopedAndAdminOnly(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "session-user", strongPassword)

	status, body := h.do(t, user, http.MethodGet, "/api/v1/sessions", nil)
	if status != http.StatusOK {
		t.Fatalf("list sessions = %d %s", status, body)
	}
	if !strings.Contains(string(body), "sessions") {
		t.Fatalf("the answer has no sessions field: %s", body)
	}

	// No device is connected, so the list must be empty rather than invented.
	if !strings.Contains(string(body), "[]") {
		t.Fatalf("expected an empty list for a user with no device: %s", body)
	}

	if status, _ := h.do(t, user, http.MethodGet, "/api/internal/sessions", nil); status != http.StatusForbidden {
		t.Fatalf("a normal user reached the internal session list: %d", status)
	}
	if status, body := h.do(t, admin, http.MethodGet, "/api/internal/sessions", nil); status != http.StatusOK {
		t.Fatalf("admin internal sessions = %d %s", status, body)
	}
}
