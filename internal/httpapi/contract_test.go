package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestResponseShapesMatchClientInterfaces asserts the JSON keys the client
// declares in client/src/lib/http.ts. A missing key is invisible in Go (the
// struct just omits it) but shows up in the browser as "undefined".
func TestResponseShapesMatchClientInterfaces(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "olga", strongPassword)

	cases := []struct {
		path string
		keys []string
	}{
		{"/api/v1/console-info", []string{"username", "role", "config_server_protocol", "webhook_auth"}},
		{"/api/v1/summary", []string{"device_count", "online_device_count"}},
		{"/api/v1/machines", []string{"machines"}},
		{"/api/v1/networks", []string{"networks"}},
	}
	for _, testCase := range cases {
		status, body := h.do(t, user, http.MethodGet, testCase.path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s = %d %s", testCase.path, status, body)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s is not a JSON object: %v", testCase.path, err)
		}
		for _, key := range testCase.keys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("%s is missing key %q (client declares it)", testCase.path, key)
			}
		}
	}

	// The machine and network entries have their own shapes.
	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "olga")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: "cafe0000-0000-0000-0000-000000000001",
		Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.66.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-shape", DisplayName: "shape", NetworkName: "shape-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	_, body := h.do(t, user, http.MethodGet, "/api/v1/machines", nil)
	var machines struct {
		Machines []map[string]any "json:\"machines\""
	}
	if err := json.Unmarshal(body, &machines); err != nil {
		t.Fatalf("decode machines: %v", err)
	}
	if len(machines.Machines) != 1 {
		t.Fatalf("machines = %d, want 1", len(machines.Machines))
	}
	for _, key := range []string{"machine_id", "hostname", "online", "running_network_instances"} {
		if _, ok := machines.Machines[0][key]; !ok {
			t.Fatalf("machine entry is missing %q", key)
		}
	}
	_, body = h.do(t, user, http.MethodGet, "/api/v1/networks", nil)
	var networks struct {
		Networks []map[string]any "json:\"networks\""
	}
	if err := json.Unmarshal(body, &networks); err != nil {
		t.Fatalf("decode networks: %v", err)
	}
	if len(networks.Networks) != 1 {
		t.Fatalf("networks = %d, want 1", len(networks.Networks))
	}
	for _, key := range []string{"network_id", "display_name", "network_name", "networking_method", "member_count"} {
		if _, ok := networks.Networks[0][key]; !ok {
			t.Fatalf("network entry is missing %q", key)
		}
	}
	// Member entries carry the sync flags the client renders.
	if err := h.store.AddMember(ctx, bal.NetworkMember{
		UserID: me.ID, NetworkID: "net-shape", ID: "member-shape",
		DeviceID:   "cafe0000-0000-0000-0000-000000000001",
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}
	_, memberBody := h.do(t, user, http.MethodGet, "/api/v1/networks/net-shape/members", nil)
	var members struct {
		Members []map[string]any "json:\"members\""
	}
	if err := json.Unmarshal(memberBody, &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members.Members) != 1 {
		t.Fatalf("members = %d, want 1", len(members.Members))
	}
	for _, key := range []string{"member_id", "device_id", "online", "temporary", "pending", "running"} {
		if _, ok := members.Members[0][key]; !ok {
			t.Fatalf("member entry is missing %q", key)
		}
	}
}
