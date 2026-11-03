package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
)

// TestDisablingRevokesOpenSessions covers F-113: disabling an account only
// refused the next login, so an already signed in user kept working for the rest
// of the session lifetime (24h by default).
func TestDisablingRevokesOpenSessions(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "frank", strongPassword)

	if status, body := h.do(t, user, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusOK {
		t.Fatalf("console-info before disable = %d %s", status, body)
	}

	me, err := h.store.GetUserByName(context.Background(), "frank")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if status, body := h.do(t, admin, http.MethodPost,
		"/api/v1/admin/users/"+strconv.FormatInt(me.ID, 10)+"/disabled",
		map[string]any{"disabled": true}); status != http.StatusNoContent {
		t.Fatalf("disable = %d %s", status, body)
	}

	// The open session is gone immediately.
	status, _ := h.do(t, user, http.MethodGet, "/api/v1/console-info", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("console-info after disable = %d, want 401", status)
	}

	// Re-enabling does not resurrect it: a fresh login is required.
	if status, body := h.do(t, admin, http.MethodPost,
		"/api/v1/admin/users/"+strconv.FormatInt(me.ID, 10)+"/disabled",
		map[string]any{"disabled": false}); status != http.StatusNoContent {
		t.Fatalf("enable = %d %s", status, body)
	}
	if status, _ := h.do(t, user, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusUnauthorized {
		t.Fatalf("console-info after re-enable = %d, want 401 until a new login", status)
	}
	fresh := h.client(t)
	if status, body := h.do(t, fresh, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "frank", "password": strongPassword,
	}); status != http.StatusOK {
		t.Fatalf("login after re-enable = %d %s", status, body)
	}
}
