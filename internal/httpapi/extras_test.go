package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestACLMemberConfigAndCredentials covers the endpoints added in r20: ACL policy
// read/write with validation, per member override config, and network
// credentials (secret shown once, never listed).
func TestACLMemberConfigAndCredentials(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	owner := h.client(t)
	h.register(t, owner, "kate", strongPassword)
	outsider := h.client(t)
	h.register(t, outsider, "leak", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "kate")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "d00d0000-0000-0000-0000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.44.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-extra", DisplayName: "extra", NetworkName: "extra-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if status, body := h.do(t, owner, http.MethodPost, "/api/v1/networks/net-extra/members", map[string]any{
		"device_ids": []string{device},
	}); status != http.StatusNoContent {
		t.Fatalf("add member = %d %s", status, body)
	}

	// ACL policy: readable when empty, rejects garbage, stores a valid one.
	if status, body := h.do(t, owner, http.MethodGet, "/api/v1/networks/net-extra/acl-policy", nil); status != http.StatusOK {
		t.Fatalf("get acl = %d %s", status, body)
	} else if !strings.Contains(string(body), "acl_policy") {
		t.Fatalf("get acl body = %s", body)
	}
	if status, _ := h.do(t, owner, http.MethodPut, "/api/v1/networks/net-extra/acl-policy", map[string]any{
		"acl_policy": "{not json",
	}); status != http.StatusBadRequest {
		t.Fatalf("invalid acl policy was accepted")
	}
	allowPolicy, err := json.Marshal(map[string]any{"default_action": "allow"})
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if status, body := h.do(t, owner, http.MethodPut, "/api/v1/networks/net-extra/acl-policy", map[string]any{
		"acl_policy": string(allowPolicy),
	}); status != http.StatusNoContent {
		t.Fatalf("put acl = %d %s", status, body)
	}

	// Member override config.
	if status, _ := h.do(t, owner, http.MethodPut, "/api/v1/networks/net-extra/members/"+device+"/config", map[string]any{
		"override_config": "{not json",
	}); status != http.StatusBadRequest {
		t.Fatalf("invalid override was accepted")
	}
	overridePayload, err := json.Marshal(map[string]any{"no_tun": true})
	if err != nil {
		t.Fatalf("marshal override: %v", err)
	}
	if status, body := h.do(t, owner, http.MethodPut, "/api/v1/networks/net-extra/members/"+device+"/config", map[string]any{
		"override_config": string(overridePayload),
	}); status != http.StatusNoContent {
		t.Fatalf("put override = %d %s", status, body)
	}
	status, body := h.do(t, owner, http.MethodGet, "/api/v1/networks/net-extra/members/"+device+"/config", nil)
	if status != http.StatusOK {
		t.Fatalf("get override = %d %s", status, body)
	}
	var override map[string]any
	if err := json.Unmarshal(body, &override); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	stored, _ := override["override_config"].(string)
	if !strings.Contains(stored, "no_tun") {
		t.Fatalf("override not stored: %q", stored)
	}

	// Credentials: the secret appears once, at creation.
	status, body = h.do(t, owner, http.MethodPost, "/api/v1/networks/net-extra/credentials", map[string]any{
		"acl_groups": []string{"ops"}, "allow_relay": true,
	})
	if status != http.StatusCreated {
		t.Fatalf("create credential = %d %s", status, body)
	}
	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	credentialID, _ := created["credential_id"].(string)
	credentialSecret, _ := created["credential_secret"].(string)
	if credentialID == "" || credentialSecret == "" {
		t.Fatalf("credential = %v", created)
	}
	_, listBody := h.do(t, owner, http.MethodGet, "/api/v1/networks/net-extra/credentials", nil)
	if strings.Contains(string(listBody), credentialSecret) {
		t.Fatalf("credential secret leaked in listing: %s", listBody)
	}
	if !strings.Contains(string(listBody), credentialID) {
		t.Fatalf("credential missing from listing: %s", listBody)
	}
	if status, _ := h.do(t, owner, http.MethodDelete, "/api/v1/networks/net-extra/credentials/"+credentialID, nil); status != http.StatusNoContent {
		t.Fatalf("revoke credential = %d", status)
	}
	if status, _ := h.do(t, owner, http.MethodDelete, "/api/v1/networks/net-extra/credentials/"+credentialID, nil); status != http.StatusNotFound {
		t.Fatalf("second revoke = %d, want 404", status)
	}

	// Tenant isolation: every new endpoint must be invisible to another user.
	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/networks/net-extra/acl-policy", nil},
		{http.MethodPut, "/api/v1/networks/net-extra/acl-policy", map[string]any{"acl_policy": ""}},
		{http.MethodGet, "/api/v1/networks/net-extra/members/" + device + "/config", nil},
		{http.MethodGet, "/api/v1/networks/net-extra/credentials", nil},
		{http.MethodPost, "/api/v1/networks/net-extra/credentials", map[string]any{}},
	}
	for _, item := range cases {
		if status, _ := h.do(t, outsider, item.method, item.path, item.body); status != http.StatusNotFound {
			t.Fatalf("%s %s as outsider = %d, want 404", item.method, item.path, status)
		}
	}
}
