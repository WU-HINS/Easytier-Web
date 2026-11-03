package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestBlockedDevicesCanBeListedAndLifted covers F-111: DELETE /machines/{id}?block=true
// used to be a one way door - nothing could list the block and nothing could lift it.
func TestBlockedDevicesCanBeListedAndLifted(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "bert", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "bert")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "beef1111-0000-4000-8000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}

	// Block through the delete flow, exactly as the UI does.
	if status, body := h.do(t, user, http.MethodDelete, "/api/v1/machines/"+device+"?block=true", nil); status != http.StatusNoContent {
		t.Fatalf("delete with block = %d %s", status, body)
	}
	blocked, err := h.store.IsBlocked(ctx, me.ID, device)
	if err != nil || !blocked {
		t.Fatalf("device was not blocked: %v %v", blocked, err)
	}

	// The block list is reachable and shows the device.
	status, body := h.do(t, user, http.MethodGet, "/api/v1/machines/blocked", nil)
	if status != http.StatusOK {
		t.Fatalf("list blocked = %d %s", status, body)
	}
	var listing struct {
		Blocked []map[string]any "json:\"blocked\""
	}
	if err := decodeInto(body, &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Blocked) != 1 || listing.Blocked[0]["machine_id"] != device {
		t.Fatalf("blocked list = %+v", listing.Blocked)
	}
	if _, ok := listing.Blocked[0]["blocked_time"]; !ok {
		t.Fatalf("blocked entry has no timestamp: %+v", listing.Blocked[0])
	}

	// Another tenant cannot see or lift it.
	outsider := h.client(t)
	h.register(t, outsider, "carl", strongPassword)
	if status, _ := h.do(t, outsider, http.MethodGet, "/api/v1/machines/blocked", nil); status != http.StatusOK {
		t.Fatalf("outsider list failed")
	}
	var outsiderListing struct {
		Blocked []map[string]any "json:\"blocked\""
	}
	_, body = h.do(t, outsider, http.MethodGet, "/api/v1/machines/blocked", nil)
	if err := decodeInto(body, &outsiderListing); err != nil {
		t.Fatalf("decode outsider: %v", err)
	}
	if len(outsiderListing.Blocked) != 0 {
		t.Fatalf("outsider saw another tenant's block list: %+v", outsiderListing.Blocked)
	}

	// Lifting the block makes the device eligible again.
	if status, body := h.do(t, user, http.MethodDelete, "/api/v1/machines/blocked/"+device, nil); status != http.StatusNoContent {
		t.Fatalf("unblock = %d %s", status, body)
	}
	blocked, err = h.store.IsBlocked(ctx, me.ID, device)
	if err != nil {
		t.Fatalf("is blocked: %v", err)
	}
	if blocked {
		t.Fatal("the device is still blocked after the unblock call")
	}
	_, body = h.do(t, user, http.MethodGet, "/api/v1/machines/blocked", nil)
	if err := decodeInto(body, &listing); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listing.Blocked) != 0 {
		t.Fatalf("blocked list not empty after unblock: %+v", listing.Blocked)
	}
}
