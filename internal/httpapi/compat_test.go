package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestOriginalConsoleAliasesAnswer covers the compatibility routes the first
// console exposed and this rewrite had dropped (docs/FEATURE-MATRIX.md).
func TestOriginalConsoleAliasesAnswer(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "compat-user", strongPassword)

	// Registration already signs the client in, so the anonymous answer needs its
	// own client: an empty cookie jar is the only way to see logged_in=false.
	anon := h.client(t)
	status, body := h.do(t, anon, http.MethodGet, "/api/v1/auth/check_login_status", nil)
	if status != http.StatusOK || !strings.Contains(string(body), "logged_in") || strings.Contains(string(body), "true") {
		t.Fatalf("anonymous check_login_status = %d %s", status, body)
	}

	// This console has no captcha; the endpoint says so instead of 404ing.
	status, body = h.do(t, user, http.MethodGet, "/api/v1/auth/captcha", nil)
	if status != http.StatusOK || !strings.Contains(string(body), "enabled") || !strings.Contains(string(body), "false") {
		t.Fatalf("captcha = %d %s", status, body)
	}

	// The original password path is a PUT on /auth/password and it is protected.
	status, _ = h.do(t, anon, http.MethodPut, "/api/v1/auth/password", map[string]any{
		"current_password": strongPassword,
		"new_password":     "Another-Strong-Pass-2!",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous put auth/password = %d, want 401", status)
	}

	// After a login the same call is accepted and the new password works.
	if s, _ := h.do(t, user, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "compat-user", "password": strongPassword}); s != http.StatusOK {
		t.Fatalf("login = %d", s)
	}
	status, body = h.do(t, user, http.MethodPut, "/api/v1/auth/password", map[string]any{
		"current_password": strongPassword,
		"new_password":     "Another-Strong-Pass-2!",
	})
	if status != http.StatusNoContent {
		t.Fatalf("put auth/password = %d %s", status, body)
	}
	fresh := h.client(t)
	if s, _ := h.do(t, fresh, http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "compat-user", "password": "Another-Strong-Pass-2!"}); s != http.StatusOK {
		t.Fatalf("login with the new password = %d", s)
	}
}
