package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestDeviceDeleteAndAlias covers F-056: deleting a device that still had
// memberships failed with a raw foreign key error, and deleting a device that
// did not exist reported success.
func TestDeviceDeleteAndAlias(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "pete", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "pete")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "bead0000-0000-0000-0000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.77.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-dev", DisplayName: "dev", NetworkName: "dev-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/net-dev/members", map[string]any{
		"device_ids": []string{device},
	}); status != http.StatusNoContent {
		t.Fatalf("add member = %d %s", status, body)
	}

	// Rename.
	if status, body := h.do(t, user, http.MethodPut, "/api/v1/machines/"+device+"/alias", map[string]any{
		"alias": "lab-node",
	}); status != http.StatusNoContent {
		t.Fatalf("alias = %d %s", status, body)
	}
	if status, _ := h.do(t, user, http.MethodPut, "/api/v1/machines/"+device+"/alias", map[string]any{
		"alias": strings.Repeat("x", 65),
	}); status != http.StatusBadRequest {
		t.Fatalf("long alias was accepted")
	}
	if status, _ := h.do(t, user, http.MethodPut, "/api/v1/machines/ffff0000-0000-0000-0000-000000000000/alias", map[string]any{
		"alias": "ghost",
	}); status != http.StatusNotFound {
		t.Fatalf("alias on unknown device = %d, want 404", status)
	}

	// Delete while it still has a membership: must succeed and take the
	// membership with it (previously a 500 with the raw foreign key text).
	status, body := h.do(t, user, http.MethodDelete, "/api/v1/machines/"+device, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete with membership = %d %s", status, body)
	}
	if lowered := strings.ToLower(string(body)); strings.Contains(lowered, "foreign key") || strings.Contains(lowered, "constraint") {
		t.Fatalf("delete leaked driver text: %s", body)
	}
	if _, err := h.store.GetDevice(ctx, me.ID, device); err == nil {
		t.Fatalf("device still present after delete")
	}
	if members, err := h.store.ListMembers(ctx, me.ID, "net-dev"); err != nil || len(members) != 0 {
		t.Fatalf("memberships after delete = %d, %v", len(members), err)
	}

	// Second delete is a 404, not a silent success.
	if status, _ := h.do(t, user, http.MethodDelete, "/api/v1/machines/"+device, nil); status != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", status)
	}
}
