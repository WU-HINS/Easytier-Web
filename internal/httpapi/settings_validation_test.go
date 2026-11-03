package httpapi_test

import (
	"net/http"
	"testing"
)

// TestAdminSettingsAreValidated covers F-013 at the contract level: the client
// validates for UX, the server must reject the same out of range values on its
// own, otherwise an admin session can weaken the policy.
func TestAdminSettingsAreValidated(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	cases := []struct {
		name  string
		patch map[string]any
		want  int
	}{
		{"negative quota", map[string]any{"max_instances_per_user": -1}, http.StatusBadRequest},
		{"quota too large", map[string]any{"max_instances_per_user": 100000}, http.StatusBadRequest},
		{"weak password floor", map[string]any{"password_min_length": 3}, http.StatusBadRequest},
		{"too many classes", map[string]any{"password_min_classes": 9}, http.StatusBadRequest},
		{"privileged port", map[string]any{"extra_port": 80}, http.StatusBadRequest},
		{"reserved name with symbol", map[string]any{"reserved_usernames": "root!,bad"}, http.StatusBadRequest},
		{"unknown key", map[string]any{"theme": "dark"}, http.StatusBadRequest},
		{"oidc without issuer", map[string]any{"oidc_enabled": true}, http.StatusBadRequest},
		{"oidc issuer not a url", map[string]any{"oidc_issuer": "not-a-url"}, http.StatusBadRequest},
		{"valid quota", map[string]any{"max_instances_per_user": 7}, http.StatusOK},
		{"valid port", map[string]any{"extra_port": 21000}, http.StatusOK},
		{"valid reserved list", map[string]any{"reserved_usernames": "admin,root,ops-team"}, http.StatusOK},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", testCase.patch)
			if status != testCase.want {
				t.Fatalf("status = %d %s, want %d", status, body, testCase.want)
			}
		})
	}

	var settings struct {
		MaxInstancesPerUser int "json:\"max_instances_per_user\""
		ExtraPort           int "json:\"extra_port\""
	}
	status, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("get settings = %d", status)
	}
	if err := decodeInto(body, &settings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if settings.MaxInstancesPerUser != 7 || settings.ExtraPort != 21000 {
		t.Fatalf("settings = %+v", settings)
	}
}
