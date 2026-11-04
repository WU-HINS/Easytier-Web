package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestConfigRoundTripsThroughToml covers the original generate-config and
// parse-config endpoints: a pushed config becomes TOML and comes back with the
// fields that matter for a connection intact.
func TestConfigRoundTripsThroughToml(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "toml-user", strongPassword)

	generate := map[string]any{"config": map[string]any{
		"hostname":       "node-a",
		"virtual_ipv4":   "10.178.0.2",
		"network_name":   "demo",
		"network_secret": "s3cret",
		"mtu":            1400,
		"no_tun":         true,
		"peer_urls":      []string{"tcp://relay.example:11010"},
		"listener_urls":  []string{"tcp://0.0.0.0:11010"},
		"proxy_cidrs":    []string{"192.168.5.0/24"},
	}}
	status, body := h.do(t, user, http.MethodPost, "/api/v1/generate-config", generate)
	if status != http.StatusOK || strings.Contains(string(body), "error") {
		t.Fatalf("generate-config = %d %s", status, body)
	}
	if !strings.Contains(string(body), "node-a") || !strings.Contains(string(body), "no_tun") {
		t.Fatalf("toml lost a field: %s", body)
	}

	// Feed the produced text back in; the parse endpoint answers with the config.
	inner := extractToml(t, body)
	status, body = h.do(t, user, http.MethodPost, "/api/v1/parse-config", map[string]any{"toml_config": inner})
	if status != http.StatusOK || strings.Contains(string(body), "error") {
		t.Fatalf("parse-config = %d %s", status, body)
	}
	for _, want := range []string{"node-a", "10.178.0.2", "demo", "tcp://relay.example:11010", "192.168.5.0/24"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("round trip lost %q: %s", want, body)
		}
	}

	// Bad TOML is reported in the body, not as a status code.
	status, body = h.do(t, user, http.MethodPost, "/api/v1/parse-config", map[string]any{"toml_config": "not = [valid"})
	if status != http.StatusOK || !strings.Contains(string(body), "error") {
		t.Fatalf("broken toml = %d %s, want 200 with an error field", status, body)
	}
}

// extractToml pulls the toml_config string out of the generate response.
func extractToml(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		TomlConfig string `json:"toml_config"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode generate response: %v", err)
	}
	return payload.TomlConfig
}
