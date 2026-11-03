package central_test

import (
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

func baseNetwork() bal.Network {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.41.0.0/29"
	return bal.Network{
		ID: "net-validate", NetworkName: "validate-net", NetworkSecret: "secret",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr, CreateTime: now, UpdateTime: now,
	}
}

func baseMember() bal.NetworkMember {
	now := time.Now().UTC().Truncate(time.Second)
	return bal.NetworkMember{
		ID: "m1", NetworkID: "net-validate", DeviceID: "dev-1", CreateTime: now, UpdateTime: now,
	}
}

func compileWith(t *testing.T, network bal.Network, member bal.NetworkMember) (central.Plan, error) {
	t.Helper()
	return central.Compile(network, []bal.NetworkMember{member},
		map[string]bal.Device{"dev-1": {MachineID: "dev-1", Hostname: "node"}}, nil)
}

// TestOverrideCannotTouchProtectedFields covers the apply_member_override rules
// from upstream: a member override must not be able to change the network
// secret, the network name, the peer list, secure mode or the ACL. Unknown keys
// are rejected outright, so a typo cannot silently change behaviour either.
func TestOverrideCannotTouchProtectedFields(t *testing.T) {
	network := baseNetwork()
	member := baseMember()
	hostile := `{"network_secret":"stolen"}`
	member.OverrideConfig = &hostile
	if _, err := compileWith(t, network, member); err == nil {
		t.Fatal("override with a protected field was accepted")
	} else if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want it to name the unknown field", err)
	}

	// Only the whitelisted fields take effect, and they do not disturb the
	// protected ones.
	overrideJSON := `{"no_tun":true,"mtu":1400,"proxy_cidrs":["192.168.10.0/24"]}`
	member.OverrideConfig = &overrideJSON
	plan, err := compileWith(t, network, member)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cfg := plan.Members[0].Config
	if cfg.GetNetworkSecret() != "secret" {
		t.Fatalf("network secret changed: %q", cfg.GetNetworkSecret())
	}
	if cfg.GetNetworkName() != "validate-net" {
		t.Fatalf("network name changed: %q", cfg.GetNetworkName())
	}
	if len(cfg.GetPeerUrls()) != 0 {
		t.Fatalf("peer urls injected: %v", cfg.GetPeerUrls())
	}
	if len(cfg.GetManagedCredentials()) != 0 {
		t.Fatalf("managed credentials injected")
	}
	if !cfg.GetNoTun() || cfg.GetMtu() != 1400 {
		t.Fatalf("whitelisted overrides were ignored: %+v", cfg)
	}
	if len(cfg.GetProxyCidrs()) != 1 || cfg.GetProxyCidrs()[0] != "192.168.10.0/24" {
		t.Fatalf("proxy cidrs = %v", cfg.GetProxyCidrs())
	}
}

// TestCompileRejectsBrokenConfigs covers the upstream validate_member_config
// step: nonsense the core would refuse must fail here, where the network page
// can show the reason.
func TestCompileRejectsBrokenConfigs(t *testing.T) {
	cases := []struct {
		name     string
		override string
		want     string
	}{
		{"bad proxy cidr", `{"proxy_cidrs":["10.0.0.0/33"]}`, "proxy cidr"},
		{"bad listener url", `{"listener_urls":["tcp//nohost"]}`, "endpoint"},
		{"listener without port", `{"listener_urls":["tcp://1.2.3.4"]}`, "port"},
		{"mtu out of range", `{"mtu":64}`, "mtu"},
	}
	for _, testCase := range cases {
		network := baseNetwork()
		member := baseMember()
		override := testCase.override
		member.OverrideConfig = &override
		if _, err := compileWith(t, network, member); err == nil {
			t.Fatalf("%s: compile accepted a broken config", testCase.name)
		} else if !strings.Contains(strings.ToLower(err.Error()), testCase.want) {
			t.Fatalf("%s: error = %v, want it to mention %q", testCase.name, err, testCase.want)
		}
	}
}

// TestCompileRejectsInvalidNetworkName makes sure the guard also fires for
// fields that come from the network row rather than the override.
func TestCompileRejectsInvalidNetworkName(t *testing.T) {
	network := baseNetwork()
	network.NetworkName = "   "
	if _, err := compileWith(t, network, baseMember()); err == nil {
		t.Fatal("compile accepted an empty network name")
	}
}

// TestACLSelectorFailsClosed covers F-069: an unknown selector type used to be
// dropped, which widened a narrow allow rule into an allow-all rule.
func TestACLSelectorFailsClosed(t *testing.T) {
	cases := map[string]string{
		"unknown type":   `{"default_action":"deny","rules":[{"action":"allow","enabled":true,"sources":[{"type":"cidr","cidrs":["10.0.0.0/8"]}]}]}`,
		"bad cidr":       `{"default_action":"deny","rules":[{"action":"allow","enabled":true,"sources":[{"type":"subnet","cidrs":["10.0.0.0/33"]}]}]}`,
		"unknown member": `{"default_action":"deny","rules":[{"action":"allow","enabled":true,"sources":[{"type":"member","member_id":"nope"}]}]}`,
	}
	for name := range cases {
		policy := cases[name]
		network := baseNetwork()
		network.ACLPolicy = &policy
		if _, err := compileWith(t, network, baseMember()); err == nil {
			t.Fatalf("%s: compile accepted a policy that cannot be enforced", name)
		}
	}
}

// TestCompileRejectsPublicServerWithoutURL covers the same rule at the compiler
// so a legacy row cannot produce an unstartable config either.
func TestCompileRejectsPublicServerWithoutURL(t *testing.T) {
	network := baseNetwork()
	network.NetworkingMethod = "PublicServer"
	if _, err := compileWith(t, network, baseMember()); err == nil {
		t.Fatal("compile accepted PublicServer without public_server_url")
	}
	url := "tcp://relay.example.com:11010"
	network.PublicServerURL = &url
	if _, err := compileWith(t, network, baseMember()); err != nil {
		t.Fatalf("compile rejected a complete PublicServer network: %v", err)
	}
}

// TestPeerURLsAreSortedAndDeduped covers the other half of F-082: upstream sorts
// and dedups peer urls, so reordering them must not look like a config change
// (which would push a new revision for no reason).
func TestPeerURLsAreSortedAndDeduped(t *testing.T) {
	network := baseNetwork()
	network.NetworkingMethod = "Manual"
	network.PeerURLs = []string{"tcp://b.example.com:11010", "tcp://a.example.com:11010", "tcp://a.example.com:11010"}

	plan, err := compileWith(t, network, baseMember())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got := plan.Members[0].Config.GetPeerUrls()
	want := []string{"tcp://a.example.com:11010", "tcp://b.example.com:11010"}
	if len(got) != len(want) {
		t.Fatalf("peer urls = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("peer urls = %v, want %v", got, want)
		}
	}

	// Reordering the same set produces the identical config.
	network.PeerURLs = []string{"tcp://a.example.com:11010", "tcp://b.example.com:11010"}
	again, err := compileWith(t, network, baseMember())
	if err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if len(again.Members[0].Config.GetPeerUrls()) != len(got) {
		t.Fatalf("reordering changed the compiled peer list")
	}
}
