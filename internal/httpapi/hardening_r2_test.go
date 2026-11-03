package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestQuotaIsPerUserNotPerNetwork covers F-011: creating more networks must not
// multiply the instance allowance.
func TestQuotaIsPerUserNotPerNetwork(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	if status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"max_instances_per_user": 1,
	}); status != http.StatusOK {
		t.Fatalf("settings = %d %s", status, body)
	}

	user := h.client(t)
	h.register(t, user, "carol", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "carol")
	if err != nil {
		t.Fatalf("carol: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, machine := range []string{"11110000-0000-0000-0000-000000000001", "11110000-0000-0000-0000-000000000002"} {
		if err := h.store.UpsertDevice(ctx, bal.Device{
			UserID: me.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device: %v", err)
		}
	}

	networkA, networkB := "net-a", "net-b"
	for _, id := range []string{networkA, networkB} {
		if err := h.store.CreateNetwork(ctx, bal.Network{
			UserID: me.ID, ID: id, DisplayName: id, NetworkName: id,
			NetworkSecret: "s", NetworkingMethod: "Standalone",
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("network %s: %v", id, err)
		}
	}

	first := map[string]any{"device_ids": []string{"11110000-0000-0000-0000-000000000001"}}
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkA+"/members", first); status != http.StatusNoContent {
		t.Fatalf("first member = %d %s", status, body)
	}
	second := map[string]any{"device_ids": []string{"11110000-0000-0000-0000-000000000002"}}
	status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkB+"/members", second)
	if status != http.StatusConflict {
		t.Fatalf("second network member = %d %s, want 409 quota_exceeded", status, body)
	}

	inUse, err := h.store.CountMembersByUser(ctx, me.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if inUse != 1 {
		t.Fatalf("members = %d, want 1", inUse)
	}
}

// TestForwardedHeaderIsNotTrustedByDefault covers F-012: rotating
// X-Forwarded-For must not defeat the login throttle.
func TestForwardedHeaderIsNotTrustedByDefault(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	attempt := func(forwarded string) int {
		body := fmt.Sprintf(`{"username":"root-owner","password":"nope"}`)
		request, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/auth/login",
			newReader(body))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", forwarded)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}

	for i := 0; i < 10; i++ {
		if status := attempt(fmt.Sprintf("10.0.0.%d", i)); status != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, status)
		}
	}
	if status := attempt("10.0.0.99"); status != http.StatusTooManyRequests {
		t.Fatalf("rotated XFF status = %d, want 429 (header must be ignored)", status)
	}

	// With trust_proxy explicitly enabled the header is honoured, for operators
	// running behind a reverse proxy.
	if err := h.store.PutSetting(context.Background(), settings.KeyTrustProxy, "true"); err != nil {
		t.Fatalf("enable trust_proxy: %v", err)
	}
	if status := attempt("10.9.9.9"); status != http.StatusUnauthorized {
		t.Fatalf("trusted proxy status = %d, want 401 for a fresh client key", status)
	}
}
