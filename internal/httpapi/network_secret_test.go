package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestNetworkSecretNeverLeavesTheServer covers F-153: a network secret is
// server side material used to derive device credentials. It must never appear in
// a JSON response, otherwise any screenshot or proxy log hands out the key.
func TestNetworkSecretNeverLeavesTheServer(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "nina", strongPassword)
	me, err := h.store.GetUserByName(ctx, "nina")
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	const secret = "known-secret-value-123"
	status, created := h.do(t, user, http.MethodPost, "/api/v1/networks", map[string]any{
		"settings": map[string]any{
			"display_name":      "secret-net",
			"networking_method": "Standalone",
			"virtual_cidr":      "10.99.0.0/29",
		},
		"network_secret": secret,
	})
	if status != http.StatusCreated {
		t.Fatalf("create = %d %s", status, created)
	}
	if strings.Contains(string(created), secret) {
		t.Fatalf("the create response echoed the secret: %s", created)
	}

	networks, err := h.store.ListNetworks(ctx, me.ID)
	if err != nil || len(networks) != 1 {
		t.Fatalf("stored networks: %d, %v", len(networks), err)
	}
	if networks[0].NetworkSecret != secret {
		t.Fatalf("the stored secret differs from the one supplied")
	}
	networkID := networks[0].ID

	reads := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/networks"},
		{http.MethodGet, "/api/v1/networks/" + networkID},
		{http.MethodGet, "/api/v1/networks/" + networkID + "/members"},
	}
	for _, read := range reads {
		status, body := h.do(t, user, read.method, read.path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s %s = %d", read.method, read.path, status)
		}
		if strings.Contains(string(body), secret) {
			t.Fatalf("%s leaked the network secret: %s", read.path, body)
		}
	}

	// The device side is the only place the material is allowed, and there it is a
	// derived key rather than the stored secret.
	_ = bal.Network{}
}
