package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// Deleting a network is a revocation. Waiting for the next reconcile tick means the
// operator gets a 204 while the device is still running the network, so the handler
// pushes before it answers (review team, r257).
func TestDeletingANetworkPushesBeforeItAnswers(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	var pushes atomic.Int32
	var hadDeadline atomic.Bool
	h.api.SetReconcileNow(func(ctx context.Context) {
		pushes.Add(1)
		if _, ok := ctx.Deadline(); ok {
			hadDeadline.Store(true)
		}
		// A slow device must not hold the response open.
		time.Sleep(50 * time.Millisecond)
	})

	status, body := h.do(t, admin, http.MethodPost, "/api/v1/networks", map[string]any{
		"settings": map[string]any{
			"display_name": "revocation-net", "networking_method": "Standalone", "virtual_cidr": "10.41.0.0/29",
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create network = %d: %s", status, body)
	}
	var created struct {
		NetworkID string `json:"network_id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode network: %v", err)
	}
	if created.NetworkID == "" {
		t.Fatalf("no network id in %s", body)
	}

	status, _ = h.do(t, admin, http.MethodDelete, "/api/v1/networks/"+created.NetworkID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", status)
	}
	if got := pushes.Load(); got != 1 {
		t.Fatalf("pushes = %d, want exactly 1", got)
	}
	if !hadDeadline.Load() {
		t.Fatal("the push ran without a deadline; a stuck device would hold the request")
	}
}

// With no hook installed the endpoint still works: the periodic reconciler remains the
// fallback, and a console built without the daemon wiring must not fail a delete.
func TestDeletingANetworkWorksWithoutAPushHook(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	status, body := h.do(t, admin, http.MethodPost, "/api/v1/networks", map[string]any{
		"settings": map[string]any{
			"display_name": "no-hook", "networking_method": "Standalone", "virtual_cidr": "10.43.0.0/29",
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create network = %d: %s", status, body)
	}
	var created struct {
		NetworkID string `json:"network_id"`
	}
	_ = json.Unmarshal(body, &created)
	status, _ = h.do(t, admin, http.MethodDelete, "/api/v1/networks/"+created.NetworkID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete without hook = %d, want 204", status)
	}
}
