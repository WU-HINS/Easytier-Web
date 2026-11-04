package central_test

import (
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestNetworkAdvancedSettingsAreInheritedAndOverridden covers the precedence rule
// decided for the network settings work: the network supplies defaults every member
// inherits, and a member override wins field by field (goal round 4).
func TestNetworkAdvancedSettingsAreInheritedAndOverridden(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.55.0.0/29"
	advanced := `{"mtu":1400,"disable_p2p":true,"listener_urls":["udp://0.0.0.0:11010"],"instance_recv_bps_limit":1000000}`
	memberOverride := `{"mtu":1200,"instance_recv_bps_limit":250000}`
	network := bal.Network{
		ID: "net-adv", NetworkName: "adv-net", NetworkSecret: "secret",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr, Advanced: &advanced,
		CreateTime: now, UpdateTime: now,
	}
	inherit := bal.NetworkMember{ID: "m1", NetworkID: "net-adv", DeviceID: "dev-1", CreateTime: now, UpdateTime: now}
	override := bal.NetworkMember{ID: "m2", NetworkID: "net-adv", DeviceID: "dev-2",
		OverrideConfig: &memberOverride, CreateTime: now.Add(time.Second), UpdateTime: now}
	devices := map[string]bal.Device{
		"dev-1": {MachineID: "dev-1", Hostname: "a"},
		"dev-2": {MachineID: "dev-2", Hostname: "b"},
	}

	plan, err := central.Compile(network, []bal.NetworkMember{inherit, override}, devices, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	byDevice := map[string]central.MemberPlan{}
	for _, member := range plan.Members {
		byDevice[member.DeviceID] = member
	}

	inherited := byDevice["dev-1"].Config
	if got := inherited.GetMtu(); got != 1400 {
		t.Fatalf("inheriting member mtu = %d, want the network value 1400", got)
	}
	if !inherited.GetDisableP2P() {
		t.Fatal("the network disable_p2p was not inherited")
	}
	if len(inherited.GetListenerUrls()) != 1 || inherited.GetListenerUrls()[0] != "udp://0.0.0.0:11010" {
		t.Fatalf("listener urls = %v, want the network value", inherited.GetListenerUrls())
	}

	overridden := byDevice["dev-2"].Config
	if got := overridden.GetMtu(); got != 1200 {
		t.Fatalf("overriding member mtu = %d, want its own 1200", got)
	}
	if got := inherited.GetInstanceRecvBpsLimit(); got != 1000000 {
		t.Fatalf("inheriting member rate limit = %d, want the network value 1000000", got)
	}
	if got := overridden.GetInstanceRecvBpsLimit(); got != 250000 {
		t.Fatalf("overriding member rate limit = %d, want its own 250000", got)
	}
	if !overridden.GetDisableP2P() {
		t.Fatal("a member override of mtu must not clear the inherited disable_p2p")
	}
}
