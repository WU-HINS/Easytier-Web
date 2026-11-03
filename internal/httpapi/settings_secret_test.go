package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestSettingsNeverReturnTheClientSecret covers F-152: the settings API reports
// whether a secret is configured, never the secret itself. A regression here
// would leak the OIDC client secret to any admin session (and to anything that
// reads the response body, such as a proxy log).
func TestSettingsNeverReturnTheClientSecret(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	const secret = "super-secret-client-value"
	if status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"oidc_client_secret": secret,
	}); status != http.StatusOK {
		t.Fatalf("save secret = %d %s", status, body)
	}

	status, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("read settings = %d %s", status, body)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("the client secret came back in the response: %s", body)
	}
	if !strings.Contains(string(body), "\"oidc_client_secret_set\":true") {
		t.Fatalf("the response does not say whether a secret is configured: %s", body)
	}
}
