package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestMemberAndNetworkEditing covers F-045: the console could add members but
// had no way to edit or remove one, and no way to change a network intent.
func TestMemberAndNetworkEditing(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "judy", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "judy")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "c0de0000-0000-0000-0000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.55.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-edit", DisplayName: "edit", NetworkName: "edit-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/net-edit/members", map[string]any{
		"device_ids": []string{device},
	}); status != http.StatusNoContent {
		t.Fatalf("add member = %d %s", status, body)
	}

	// Rename the member.
	if status, body := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-edit/members/"+device, map[string]any{
		"hostname_override": "renamed",
	}); status != http.StatusNoContent {
		t.Fatalf("patch member = %d %s", status, body)
	}
	var listing struct {
		Members []struct {
			DeviceID string "json:\"device_id\""
			Hostname string "json:\"hostname\""
		} "json:\"members\""
	}
	if status, body := h.do(t, user, http.MethodGet, "/api/v1/networks/net-edit/members", nil); status != http.StatusOK {
		t.Fatalf("list = %d %s", status, body)
	} else if err := decodeInto(body, &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Members) != 1 || listing.Members[0].Hostname != "renamed" {
		t.Fatalf("members after patch = %+v", listing.Members)
	}

	// Unknown device is a 404, not a 500.
	if status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-edit/members/ffff0000-0000-0000-0000-000000000000",
		map[string]any{"hostname_override": "x"}); status != http.StatusNotFound {
		t.Fatalf("patch unknown member = %d, want 404", status)
	}

	// Patch the network itself.
	if status, body := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-edit", map[string]any{
		"display_name": "edited",
	}); status != http.StatusOK {
		t.Fatalf("patch network = %d %s", status, body)
	}
	if status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-edit", map[string]any{
		"display_name": "  ",
	}); status != http.StatusBadRequest {
		t.Fatalf("empty display name should be rejected")
	}
	if status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-missing", map[string]any{
		"display_name": "x",
	}); status != http.StatusNotFound {
		t.Fatalf("patch unknown network = %d, want 404", status)
	}
	// An unparsable ACL policy must be rejected at write time, otherwise the
	// compile step fails later and every push for the tenant stops silently.
	if status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-edit", map[string]any{
		"acl_policy": "{not json",
	}); status != http.StatusBadRequest {
		t.Fatalf("patch with invalid acl policy = %d, want 400", status)
	}

	// Remove the member, then remove it again.
	if status, body := h.do(t, user, http.MethodDelete, "/api/v1/networks/net-edit/members/"+device, nil); status != http.StatusNoContent {
		t.Fatalf("delete member = %d %s", status, body)
	}
	if status, _ := h.do(t, user, http.MethodDelete, "/api/v1/networks/net-edit/members/"+device, nil); status != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", status)
	}
	if members, err := h.store.ListMembers(ctx, me.ID, "net-edit"); err != nil || len(members) != 0 {
		t.Fatalf("members after delete = %v, %v", len(members), err)
	}
}

// TestMemberPendingFlag covers F-087: the members list must say whether the
// device actually has the current configuration, so a failed or pending sync is
// visible instead of invisible.
func TestMemberPendingFlag(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "zoe", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "zoe")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "abcd3333-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.93.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-pending", DisplayName: "pending", NetworkName: "pending-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := h.store.AddMember(ctx, bal.NetworkMember{
		UserID: me.ID, NetworkID: "net-pending", ID: "member-1", DeviceID: device,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}

	read := func() map[string]any {
		status, body := h.do(t, user, http.MethodGet, "/api/v1/networks/net-pending/members", nil)
		if status != http.StatusOK {
			t.Fatalf("list members = %d %s", status, body)
		}
		var payload struct {
			Members []map[string]any "json:\"members\""
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(payload.Members) != 1 {
			t.Fatalf("members = %d", len(payload.Members))
		}
		return payload.Members[0]
	}

	if pending, _ := read()["pending"].(bool); !pending {
		t.Fatalf("a member that was never synced must report pending")
	}

	// Record the revision the console would push, as the reconciler does.
	network, err := h.store.GetNetwork(ctx, me.ID, "net-pending")
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	members, err := h.store.ListMembers(ctx, me.ID, "net-pending")
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	plan, err := central.Compile(network, members, map[string]bal.Device{device: {MachineID: device, Hostname: "node"}}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	revision := central.PlanRevision("net-pending", plan.Members[0])
	if err := h.store.SetManagedConfigRevision(ctx, me.ID, device, "{\"net-pending\":\""+revision+"\"}", nil); err != nil {
		t.Fatalf("set revision: %v", err)
	}
	if pending, _ := read()["pending"].(bool); pending {
		t.Fatalf("a synced member must not report pending")
	}
}
