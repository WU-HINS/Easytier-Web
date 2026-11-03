package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestDeletingAForeignNetworkIsNotFound covers F-217: the delete used to answer
// 204 whether or not the row existed, so a wrong or foreign id looked like a
// successful delete (found by the review team in r135).
func TestDeletingAForeignNetworkIsNotFound(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	owner := h.client(t)
	h.register(t, owner, "kim", strongPassword)
	intruder := h.client(t)
	h.register(t, intruder, "lee", strongPassword)
	me, err := h.store.GetUserByName(ctx, "kim")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.70.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-owned", DisplayName: "owned", NetworkName: "owned-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}

	status, body := h.do(t, intruder, http.MethodDelete, "/api/v1/networks/net-owned", nil)
	if status != http.StatusNotFound {
		t.Fatalf("foreign delete = %d %s, want 404", status, body)
	}
	if _, err := h.store.GetNetwork(ctx, me.ID, "net-owned"); err != nil {
		t.Fatalf("the owner lost the network: %v", err)
	}

	status, body = h.do(t, owner, http.MethodDelete, "/api/v1/networks/net-owned", nil)
	if status != http.StatusNoContent {
		t.Fatalf("own delete = %d %s, want 204", status, body)
	}
	status, body = h.do(t, owner, http.MethodDelete, "/api/v1/networks/net-owned", nil)
	if status != http.StatusNotFound {
		t.Fatalf("second delete = %d %s, want 404", status, body)
	}
}

// TestBlockingAForeignMachineLeavesNoTrace covers the ordering half of F-217: the
// block used to be written before ownership was checked, so a rejected request
// still left a row in blocked_devices.
func TestBlockingAForeignMachineLeavesNoTrace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	owner := h.client(t)
	h.register(t, owner, "morgan", strongPassword)
	intruder := h.client(t)
	h.register(t, intruder, "neddy", strongPassword)
	me, err := h.store.GetUserByName(ctx, "morgan")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const machine = "beef9200-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: machine, Hostname: "owned-node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}

	status, body := h.do(t, intruder, http.MethodDelete, "/api/v1/machines/"+machine+"?block=true", nil)
	if status != http.StatusNotFound {
		t.Fatalf("foreign block = %d %s, want 404", status, body)
	}
	blocked, err := h.store.ListBlockedDevices(ctx, mustUserID(t, h, "neddy"))
	if err != nil {
		t.Fatalf("blocked list: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("the rejected request left %d blocked row(s)", len(blocked))
	}
	if _, err := h.store.GetDevice(ctx, me.ID, machine); err != nil {
		t.Fatalf("the owner lost the device: %v", err)
	}

	status, body = h.do(t, intruder, http.MethodDelete, "/api/v1/machines/blocked/"+machine, nil)
	if status != http.StatusNotFound {
		t.Fatalf("unblock nothing = %d %s, want 404", status, body)
	}
}

func mustUserID(t *testing.T, h *harness, name string) int64 {
	t.Helper()
	user, err := h.store.GetUserByName(context.Background(), name)
	if err != nil {
		t.Fatalf("user %s: %v", name, err)
	}
	return user.ID
}
