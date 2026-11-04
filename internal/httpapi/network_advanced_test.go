package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestNetworkAdvancedSettingsRoundTrip covers the API half of the network settings
// work: the blob is stored verbatim, an unparsable one is refused, and a whitelist
// the reconciler could not afford is refused too.
func TestNetworkAdvancedSettingsRoundTrip(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "adv-user", strongPassword)
	me, err := h.store.GetUserByName(ctx, "adv-user")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.72.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-adv", DisplayName: "adv", NetworkName: "adv-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}

	blob := `{"mtu":1400,"instance_recv_bps_limit":1000000,"whitelist":["*"]}`
	status, body := h.do(t, user, http.MethodPut, "/api/v1/networks/net-adv/advanced",
		map[string]any{"advanced": blob})
	if status != http.StatusNoContent {
		t.Fatalf("put advanced = %d %s, want 204", status, body)
	}

	status, body = h.do(t, user, http.MethodGet, "/api/v1/networks/net-adv/advanced", nil)
	if status != http.StatusOK {
		t.Fatalf("get advanced = %d", status)
	}
	if !strings.Contains(string(body), "1400") || !strings.Contains(string(body), "1000000") {
		t.Fatalf("advanced round trip lost a field: %s", body)
	}

	if status, _ := h.do(t, user, http.MethodPut, "/api/v1/networks/net-adv/advanced",
		map[string]any{"advanced": "{not json"}); status != http.StatusBadRequest {
		t.Fatalf("unparsable advanced = %d, want 400", status)
	}

	tooMany := strings.Repeat(`,"a"`, 300)
	tooMany = tooMany[1:]
	oversized := `{"whitelist":[` + tooMany + "]}"
	if status, _ := h.do(t, user, http.MethodPut, "/api/v1/networks/net-adv/advanced",
		map[string]any{"advanced": oversized}); status != http.StatusBadRequest {
		t.Fatalf("oversized whitelist = %d, want 400", status)
	}

	stored, err := h.store.GetNetwork(ctx, me.ID, "net-adv")
	if err != nil {
		t.Fatalf("reload network: %v", err)
	}
	if stored.Advanced == nil || *stored.Advanced != blob {
		t.Fatalf("the refused payload replaced the stored blob: %v", stored.Advanced)
	}
}
