package httpapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestConcurrentMemberPatchesKeepBothFields covers F-228: the update used to read
// the whole row, change one field and write every column back, so two concurrent
// patches each undid the other while both callers saw 204 (review team, r140).
func TestConcurrentMemberPatchesKeepBothFields(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "patch-user", strongPassword)
	me, err := h.store.GetUserByName(ctx, "patch-user")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const machine = "beef9300-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.71.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-patch", DisplayName: "patch", NetworkName: "patch-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := h.store.AddMembersBounded(ctx, me.ID, []bal.NetworkMember{{
		UserID: me.ID, NetworkID: "net-patch", ID: "member-1", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}}, 10); err != nil {
		t.Fatalf("member: %v", err)
	}

	payloads := []map[string]any{
		{"hostname_override": "gw-a"},
		{"virtual_ipv4": "10.71.0.2"},
	}
	codes := make([]int, len(payloads))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index, payload := range payloads {
		wg.Add(1)
		go func(slot int, body map[string]any) {
			defer wg.Done()
			<-start
			status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/net-patch/members/"+machine, body)
			codes[slot] = status
		}(index, payload)
	}
	close(start)
	wg.Wait()
	for slot, code := range codes {
		if code != http.StatusNoContent {
			t.Fatalf("writer %d = %d, want 204", slot, code)
		}
	}
	member, err := h.store.GetMemberByDevice(ctx, me.ID, "net-patch", machine)
	if err != nil {
		t.Fatalf("read member: %v", err)
	}
	if member.HostnameOverride == nil || *member.HostnameOverride != "gw-a" {
		t.Fatalf("hostname_override = %v, the other patch undid it", member.HostnameOverride)
	}
	if member.VirtualIPv4 == nil || *member.VirtualIPv4 != "10.71.0.2" {
		t.Fatalf("virtual_ipv4 = %v, the other patch undid it", member.VirtualIPv4)
	}
}

// TestMemberPatchRejectsAnUnparsableOverride covers F-238: the PATCH path wrote
// override_config without the validation the PUT path applies, so one bad blob
// stopped every push for the whole tenant (review team, round 150).
func TestMemberPatchRejectsAnUnparsableOverride(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "override-user", strongPassword)
	me, err := h.store.GetUserByName(ctx, "override-user")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const machine = "beef9400-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.72.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-override", DisplayName: "override", NetworkName: "override-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := h.store.AddMembersBounded(ctx, me.ID, []bal.NetworkMember{{
		UserID: me.ID, NetworkID: "net-override", ID: "member-1", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}}, 10); err != nil {
		t.Fatalf("member: %v", err)
	}

	status, body := h.do(t, user, http.MethodPatch,
		"/api/v1/networks/net-override/members/"+machine,
		map[string]any{"override_config": "{not json"})
	if status != http.StatusBadRequest {
		t.Fatalf("unparsable override = %d %s, want 400", status, body)
	}
	member, err := h.store.GetMemberByDevice(ctx, me.ID, "net-override", machine)
	if err != nil {
		t.Fatalf("read member: %v", err)
	}
	if member.OverrideConfig != nil {
		t.Fatalf("the bad override was stored anyway: %v", *member.OverrideConfig)
	}
}
