package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestNoEndpointEchoesKnownSecrets sweeps every read endpoint the console uses
// and asserts none of the known server side secrets appear. Per endpoint tests
// exist for the two most sensitive ones; this one catches the next endpoint that
// is added and happens to include a whole row.
func TestNoEndpointEchoesKnownSecrets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "olive", strongPassword)
	me, err := h.store.GetUserByName(ctx, "olive")
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	const networkSecret = "NET-SECRET-MARKER-9f1"
	status, _ := h.do(t, user, http.MethodPost, "/api/v1/networks", map[string]any{
		"settings": map[string]any{
			"display_name":      "sweep",
			"networking_method": "Standalone",
			"virtual_cidr":      "10.77.0.0/29",
		},
		"network_secret": networkSecret,
	})
	if status != http.StatusCreated {
		t.Fatalf("create network = %d", status)
	}
	networks, err := h.store.ListNetworks(ctx, me.ID)
	if err != nil || len(networks) != 1 {
		t.Fatalf("stored networks: %d %v", len(networks), err)
	}
	networkID := networks[0].ID

	// A credential secret is shown exactly once, at creation.
	status, created := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/credentials", map[string]any{
		"acl_groups":  []string{"ops"},
		"allow_relay": true,
	})
	if status != http.StatusCreated {
		t.Fatalf("create credential = %d %s", status, created)
	}
	const marker = "credential_secret\":\""
	at := strings.Index(string(created), marker)
	if at < 0 {
		t.Fatalf("the creation response did not hand out the secret: %s", created)
	}
	rest := string(created)[at+len(marker):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		t.Fatalf("cannot parse the credential secret: %s", created)
	}
	credentialSecret := rest[:end]
	if len(credentialSecret) < 8 {
		t.Fatalf("implausible credential secret: %q", credentialSecret)
	}

	// A device, so the sweep covers member shaped responses.
	now := time.Now().UTC().Truncate(time.Second)
	machine := "beef7000-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": []string{machine},
	}); status != http.StatusNoContent {
		t.Fatalf("add member = %d %s", status, body)
	}

	secrets := []string{networkSecret, credentialSecret}
	paths := []string{
		"/api/v1/console-info",
		"/api/v1/summary",
		"/api/v1/machines",
		"/api/v1/machines/blocked",
		"/api/v1/networks",
		"/api/v1/networks/" + networkID,
		"/api/v1/networks/" + networkID + "/members",
		"/api/v1/networks/" + networkID + "/credentials",
		"/api/v1/networks/" + networkID + "/acl-policy",
		"/api/v1/networks/" + networkID + "/members/" + machine + "/config",
		"/api/v1/account/oidc",
		"/api/v1/admin/users",
		"/api/v1/admin/settings",
		"/api/v1/rpc-capabilities",
	}
	for _, path := range paths {
		client := user
		if strings.HasPrefix(path, "/api/v1/admin/") {
			client = admin
		}
		status, body := h.do(t, client, http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s = %d %s", path, status, body)
		}
		for _, secret := range secrets {
			if strings.Contains(string(body), secret) {
				t.Fatalf("%s leaked a secret: %s", path, body)
			}
		}
	}
}
