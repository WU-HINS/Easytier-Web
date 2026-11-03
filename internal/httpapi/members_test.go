package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

func seedMemberScenario(t *testing.T, h *harness) (networkID string, devices []string) {
	t.Helper()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "erin", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "erin")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	devices = []string{
		"aaaa0000-0000-0000-0000-00000000000a",
		"aaaa0000-0000-0000-0000-00000000000b",
	}
	for _, device := range devices {
		if err := h.store.UpsertDevice(ctx, bal.Device{
			UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device: %v", err)
		}
	}
	networkID = "net-members"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: networkID, DisplayName: networkID, NetworkName: networkID,
		NetworkSecret: "s", NetworkingMethod: "Standalone", CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	return networkID, devices
}

func countMembers(t *testing.T, h *harness, user, networkID string) int {
	t.Helper()
	me, err := h.store.GetUserByName(context.Background(), user)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	members, err := h.store.ListMembers(context.Background(), me.ID, networkID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	return len(members)
}

// TestAddMembersIsAtomicAndTyped covers F-019 / F-020: a bad batch must fail as
// a whole, with a typed status instead of a 500 that leaks the schema.
func TestAddMembersIsAtomicAndTyped(t *testing.T) {
	h := newHarness(t)
	networkID, devices := seedMemberScenario(t, h)
	user := h.client(t)
	if status, _ := h.do(t, user, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "erin", "password": strongPassword,
	}); status != http.StatusOK {
		t.Fatalf("login failed")
	}

	// 1. Duplicate inside one request is rejected before any write.
	status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": []string{devices[0], devices[0]},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("duplicate payload = %d %s, want 400", status, body)
	}
	if got := countMembers(t, h, "erin", networkID); got != 0 {
		t.Fatalf("members after duplicate request = %d, want 0", got)
	}

	// 2. A batch containing an unknown device is not partially applied.
	status, body = h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": []string{devices[0], "ffff0000-0000-0000-0000-000000000000"},
	})
	if status != http.StatusNotFound {
		t.Fatalf("unknown device = %d %s, want 404", status, body)
	}
	if got := countMembers(t, h, "erin", networkID); got != 0 {
		t.Fatalf("members after failed batch = %d, want 0 (batch must be atomic)", got)
	}

	// 3. The happy path still works.
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": devices,
	}); status != http.StatusNoContent {
		t.Fatalf("valid batch = %d %s", status, body)
	}
	if got := countMembers(t, h, "erin", networkID); got != 2 {
		t.Fatalf("members = %d, want 2", got)
	}

	// 4. Adding the same device again is a conflict, not a server error, and the
	// response must not name tables or columns.
	status, body = h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": []string{devices[0]},
	})
	if status != http.StatusConflict {
		t.Fatalf("existing member = %d %s, want 409", status, body)
	}
	lower := strings.ToLower(string(body))
	for _, leak := range []string{"unique", "constraint", "network_members", "sqlstate"} {
		if strings.Contains(lower, leak) {
			t.Fatalf("response leaks driver detail %q: %s", leak, body)
		}
	}
}
