package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestCrossOriginWriteGuard covers F-097: mutating requests carrying a foreign
// Origin must be rejected. Without a test, a broken sameOrigin comparison would
// silently remove the CSRF defence.
func TestCrossOriginWriteGuard(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	post := func(origin string) (int, string) {
		body, _ := json.Marshal(map[string]string{"username": "root-owner", "password": "wrong-password"})
		request, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer response.Body.Close()
		payload := make([]byte, 256)
		n, _ := response.Body.Read(payload)
		return response.StatusCode, string(payload[:n])
	}

	host := strings.TrimPrefix(h.server.URL, "http://")

	// A foreign origin is refused before the handler runs.
	status, body := post("http://evil.example")
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin write = %d %s, want 403", status, body)
	}
	if !strings.Contains(body, "cross-origin write rejected") {
		t.Fatalf("cross-origin write rejected for the wrong reason: %s", body)
	}

	// The console's own origin is allowed (the request then fails on the password,
	// which proves the guard let it through).
	if status, _ := post(h.server.URL); status != http.StatusUnauthorized {
		t.Fatalf("same-origin write = %d, want 401 from the handler", status)
	}

	// Non-browser clients send no Origin and must keep working.
	if status, _ := post(""); status != http.StatusUnauthorized {
		t.Fatalf("origin-less write = %d, want 401 from the handler", status)
	}

	// Reads are never blocked by the origin rule.
	request, _ := http.NewRequest(http.MethodGet, h.server.URL+"/healthz", nil)
	request.Header.Set("Origin", "http://evil.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cross-origin GET = %d, want 200", response.StatusCode)
	}
	_ = host
}

// TestDisabledAccountCannotLogin covers F-098: the disabled check must run after
// the password check, otherwise it leaks which accounts exist.
func TestDisabledAccountCannotLogin(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "dora", strongPassword)

	me, err := h.store.GetUserByName(context.Background(), "dora")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if status, body := h.do(t, admin, http.MethodPost, "/api/v1/admin/users/"+strconv.FormatInt(me.ID, 10)+"/disabled",
		map[string]any{"disabled": true}); status != http.StatusNoContent {
		t.Fatalf("disable = %d %s", status, body)
	}

	fresh := h.client(t)
	status, body := h.do(t, fresh, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "dora", "password": strongPassword,
	})
	if status != http.StatusForbidden || !strings.Contains(string(body), "account is disabled") {
		t.Fatalf("disabled login = %d %s, want 403 account is disabled", status, body)
	}

	// A wrong password on a disabled account still looks like a wrong password.
	status, body = h.do(t, h.client(t), http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "dora", "password": "definitely-wrong",
	})
	if status != http.StatusUnauthorized || strings.Contains(string(body), "disabled") {
		t.Fatalf("wrong password on a disabled account = %d %s, must not reveal the state", status, body)
	}
}

func itoa64(value int64) string { return strconv.FormatInt(value, 10) }
