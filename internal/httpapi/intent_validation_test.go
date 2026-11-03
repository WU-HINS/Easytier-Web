package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestNetworkIntentIsValidatedOnWrite covers F-050: an intent the compiler
// cannot turn into a config used to be stored and then silently stopped every
// push for the tenant.
func TestNetworkIntentIsValidatedOnWrite(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "mia", strongPassword)

	base := map[string]any{"display_name": "intent", "networking_method": "Standalone"}
	counter := 0

	cases := []struct {
		name     string
		settings map[string]any
		want     int
	}{
		{"missing cidr", base, http.StatusBadRequest},
		{"empty cidr", merge(base, "virtual_cidr", ""), http.StatusBadRequest},
		{"garbage cidr", merge(base, "virtual_cidr", "not-a-cidr"), http.StatusBadRequest},
		{"ipv6 cidr", merge(base, "virtual_cidr", "fd00::/64"), http.StatusBadRequest},
		{"prefix too wide", merge(base, "virtual_cidr", "10.0.0.0/32"), http.StatusBadRequest},
		{"manual without peers", merge(merge(base, "networking_method", "Manual"), "virtual_cidr", "10.20.0.0/29"), http.StatusBadRequest},
		{"public server bad url", merge(merge(base, "networking_method", "PublicServer"), "public_server_url", "tcp//example.com"), http.StatusBadRequest},
		{"unknown method", merge(merge(base, "networking_method", "Wireguard"), "virtual_cidr", "10.20.0.0/29"), http.StatusBadRequest},
		{"valid standalone", merge(base, "virtual_cidr", "10.20.0.0/29"), http.StatusOK},
		{"valid manual", merge(merge(base, "networking_method", "Manual"), "virtual_cidr", "10.21.0.0/29"), http.StatusOK},
	}
	for index, testCase := range cases {
		counter++
		testCase.settings["display_name"] = fmt.Sprintf("intent-%02d", counter)
		if index == 9 {
			testCase.settings["peer_urls"] = []string{"tcp://127.0.0.1:11010"}
		}
		status, body := h.do(t, user, http.MethodPost, "/api/v1/networks", map[string]any{"settings": testCase.settings})
		if status != testCase.want && !(testCase.want == http.StatusOK && status == http.StatusCreated) {
			t.Fatalf("%s: status = %d body = %s, want %d", testCase.name, status, body, testCase.want)
		}
	}
}

// TestMemberAddressMustBeInsideSubnet covers F-051: an out of subnet address is
// accepted by the compiler and pushed to the device, producing a config no peer
// can reach.
func TestMemberAddressMustBeInsideSubnet(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "nina", strongPassword)

	ctx := context.Background()
	me, err := h.store.GetUserByName(ctx, "nina")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	device := "feed0000-0000-0000-0000-000000000001"
	if err := h.store.UpsertDevice(ctx, bal.Device{
		UserID: me.ID, MachineID: device, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	created := map[string]any{"settings": map[string]any{
		"display_name": "subnet", "networking_method": "Standalone", "virtual_cidr": "10.30.0.0/29",
	}}
	status, body := h.do(t, user, http.MethodPost, "/api/v1/networks", created)
	if status != http.StatusCreated {
		t.Fatalf("create network = %d %s", status, body)
	}
	networkID := extractField(body, "network_id")
	if networkID == "" {
		t.Fatalf("network id missing: %s", body)
	}
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/"+networkID+"/members", map[string]any{
		"device_ids": []string{device},
	}); status != http.StatusNoContent {
		t.Fatalf("add member = %d %s", status, body)
	}
	// Outside the /29: rejected.
	if status, body := h.do(t, user, http.MethodPatch, "/api/v1/networks/"+networkID+"/members/"+device, map[string]any{
		"virtual_ipv4": "10.99.99.99",
	}); status != http.StatusBadRequest {
		t.Fatalf("out of subnet address = %d %s, want 400", status, body)
	}
	// Inside the /29: accepted.
	if status, body := h.do(t, user, http.MethodPatch, "/api/v1/networks/"+networkID+"/members/"+device, map[string]any{
		"virtual_ipv4": "10.30.0.3",
	}); status != http.StatusNoContent {
		t.Fatalf("in subnet address = %d %s", status, body)
	}
	// Not an address at all.
	if status, _ := h.do(t, user, http.MethodPatch, "/api/v1/networks/"+networkID+"/members/"+device, map[string]any{
		"virtual_ipv4": "not-an-ip",
	}); status != http.StatusBadRequest {
		t.Fatalf("garbage address was accepted")
	}
}

func merge(base map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

// TestPublicServerRequiresURL covers F-079: the core turns PublicServer into the
// single peer it dials, so an empty URL fails to parse there and the instance
// never starts. The API used to accept it.
func TestPublicServerRequiresURL(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "uma", strongPassword)

	status, body := h.do(t, user, http.MethodPost, "/api/v1/networks", map[string]any{"settings": map[string]any{
		"display_name": "public-no-url", "networking_method": "PublicServer", "virtual_cidr": "10.70.0.0/29",
	}})
	if status != http.StatusBadRequest {
		t.Fatalf("PublicServer without url = %d %s, want 400", status, body)
	}
	if !strings.Contains(strings.ToLower(string(body)), "public_server_url") {
		t.Fatalf("error should name the missing field: %s", body)
	}
}

// TestManualAndGatewayPeerRules covers F-082: upstream rejects Manual without
// peer_urls and requires a URL for Gateway; it does not accept a public server
// URL as a substitute for the Manual peer list.
func TestManualAndGatewayPeerRules(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "vera", strongPassword)

	cases := []struct {
		name     string
		settings map[string]any
		want     int
	}{
		{"manual with only a public server url", map[string]any{
			"display_name": "m1", "networking_method": "Manual", "virtual_cidr": "10.71.0.0/29",
			"public_server_url": "tcp://relay.example.com:11010",
		}, http.StatusBadRequest},
		{"manual with peers", map[string]any{
			"display_name": "m2", "networking_method": "Manual", "virtual_cidr": "10.71.1.0/29",
			"peer_urls": []string{"tcp://relay.example.com:11010"},
		}, http.StatusCreated},
		{"gateway without url", map[string]any{
			"display_name": "g1", "networking_method": "Gateway", "virtual_cidr": "10.71.2.0/29",
			"peer_urls": []string{"tcp://relay.example.com:11010"},
		}, http.StatusBadRequest},
		{"gateway with url", map[string]any{
			"display_name": "g2", "networking_method": "Gateway", "virtual_cidr": "10.71.3.0/29",
			"public_server_url": "tcp://gateway.example.com:11010",
		}, http.StatusCreated},
	}
	for _, testCase := range cases {
		status, body := h.do(t, user, http.MethodPost, "/api/v1/networks", map[string]any{"settings": testCase.settings})
		if status != testCase.want {
			t.Fatalf("%s: status = %d body = %s, want %d", testCase.name, status, body, testCase.want)
		}
	}
}
