package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/oidc/oidctest"
)

// TestOIDCSettingsTakeEffectAtRuntime covers the admin path that never needs a
// restart: enabling SSO in the settings page must make the login button appear,
// and disabling it must make the endpoints disappear again.
func TestOIDCSettingsTakeEffectAtRuntime(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()

	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	// The fake provider speaks http on loopback.
	if err := h.store.PutSetting(context.Background(), "oidc.allow_insecure", "true"); err != nil {
		t.Fatalf("allow insecure: %v", err)
	}

	status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"oidc_enabled":       true,
		"oidc_issuer":        idp.Server.URL,
		"oidc_client_id":     idp.ClientID,
		"oidc_client_secret": "secret",
		"oidc_button_label":  "Continue with SSO",
	})
	if status != http.StatusOK {
		t.Fatalf("enable oidc = %d %s", status, body)
	}

	readConfig := func() map[string]any {
		status, body := h.do(t, h.client(t), http.MethodGet, "/api/v1/auth/oidc/config", nil)
		if status != http.StatusOK {
			t.Fatalf("oidc config = %d %s", status, body)
		}
		var payload map[string]any
		if err := decodeInto(body, &payload); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return payload
	}

	config := readConfig()
	if enabled, _ := config["enabled"].(bool); !enabled {
		t.Fatalf("sso did not become active: %+v", config)
	}
	if label, _ := config["button_label"].(string); label != "Continue with SSO" {
		t.Fatalf("button label = %q", config["button_label"])
	}

	if status, body := h.do(t, h.client(t), http.MethodGet, "/api/v1/auth/oidc/login", nil); status != http.StatusFound {
		t.Fatalf("login = %d %s, want a redirect to the provider", status, body)
	}

	// Disabling removes the endpoints again.
	if status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"oidc_enabled": false,
	}); status != http.StatusOK {
		t.Fatalf("disable oidc = %d %s", status, body)
	}
	config = readConfig()
	if enabled, _ := config["enabled"].(bool); enabled {
		t.Fatalf("sso still active after disabling: %+v", config)
	}
	if status, _ := h.do(t, h.client(t), http.MethodGet, "/api/v1/auth/oidc/login", nil); status != http.StatusNotFound {
		t.Fatalf("login after disable = %d, want 404", status)
	}
}

// TestOIDCSettingsReportReloadFailure pins the documented wording: the setting is
// saved, but the operator is told the provider could not be rebuilt.
func TestOIDCSettingsReportReloadFailure(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"oidc_enabled":   true,
		"oidc_issuer":    "https://127.0.0.1:1/unreachable",
		"oidc_client_id": "client",
	})
	if status != http.StatusBadGateway {
		t.Fatalf("unreachable issuer = %d %s, want 502", status, body)
	}
	if !strings.Contains(string(body), "settings saved but OIDC reload failed") {
		t.Fatalf("unexpected message: %s", body)
	}
	// A reload failure must not take the REST surface down.
	if status, _ := h.do(t, admin, http.MethodGet, "/api/v1/admin/settings", nil); status != http.StatusOK {
		t.Fatalf("settings unreadable after a failed reload")
	}
}
