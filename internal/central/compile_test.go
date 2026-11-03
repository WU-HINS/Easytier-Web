package central_test

import (
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/acl"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
)

func fixture() (bal.Network, []bal.NetworkMember) {
	cidr := "10.42.0.0/29"
	now := time.Now()
	network := bal.Network{
		ID:               "net-1",
		DisplayName:      "lab",
		NetworkName:      "lab-net",
		NetworkSecret:    "s3cret",
		NetworkingMethod: "Manual",
		PeerURLs:         []string{"tcp://1.2.3.4:11010"},
		VirtualCIDR:      &cidr,
		CreateTime:       now,
		UpdateTime:       now,
	}
	members := []bal.NetworkMember{
		{ID: "m1", DeviceID: "dev-1", CreateTime: now.Add(time.Second), UpdateTime: now},
		{ID: "m2", DeviceID: "dev-2", CreateTime: now.Add(2 * time.Second), UpdateTime: now},
		{ID: "m3", DeviceID: "dev-3", CreateTime: now.Add(3 * time.Second), UpdateTime: now},
	}
	return network, members
}

func TestCompileAllocatesSequentialAddresses(t *testing.T) {
	network, members := fixture()
	devices := map[string]bal.Device{
		"dev-1": {MachineID: "dev-1", Hostname: "alpha"},
		"dev-2": {MachineID: "dev-2", Hostname: "bravo"},
		"dev-3": {MachineID: "dev-3", Hostname: "charlie"},
	}
	plan, err := central.Compile(network, members, devices, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plan.Members) != 3 {
		t.Fatalf("members = %d", len(plan.Members))
	}
	want := []string{"10.42.0.1", "10.42.0.2", "10.42.0.3"}
	for i, member := range plan.Members {
		if member.VirtualIPv4 != want[i] {
			t.Fatalf("member %d ip = %s, want %s", i, member.VirtualIPv4, want[i])
		}
		if member.Config.GetNetworkName() != "lab-net" {
			t.Fatalf("network name = %s", member.Config.GetNetworkName())
		}
		if member.Config.GetNetworkingMethod() != manage.NetworkingMethod_Manual {
			t.Fatalf("method = %v", member.Config.GetNetworkingMethod())
		}
		if member.Config.GetNetworkLength() != 29 {
			t.Fatalf("prefix = %d", member.Config.GetNetworkLength())
		}
	}
	if plan.Members[0].Hostname != "alpha" {
		t.Fatalf("hostname = %s", plan.Members[0].Hostname)
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	network, members := fixture()
	first, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile twice: %v", err)
	}
	for i := range first.Members {
		if first.Members[i].InstanceID != second.Members[i].InstanceID {
			t.Fatalf("instance id not stable: %s vs %s",
				first.Members[i].InstanceID, second.Members[i].InstanceID)
		}
		if first.Members[i].VirtualIPv4 != second.Members[i].VirtualIPv4 {
			t.Fatal("address allocation not stable")
		}
	}
	if first.Members[0].InstanceID == first.Members[1].InstanceID {
		t.Fatal("instance ids collided across devices")
	}
}

func TestCompileHonoursExplicitAddresses(t *testing.T) {
	network, members := fixture()
	explicit := "10.42.0.6"
	members[0].VirtualIPv4 = &explicit
	plan, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if plan.Members[0].VirtualIPv4 != explicit {
		t.Fatalf("explicit ip lost: %s", plan.Members[0].VirtualIPv4)
	}
	for _, member := range plan.Members[1:] {
		if member.VirtualIPv4 == explicit {
			t.Fatal("explicit address was handed out twice")
		}
	}
}

func TestCompileRejectsDuplicateAddresses(t *testing.T) {
	network, members := fixture()
	dup := "10.42.0.5"
	members[0].VirtualIPv4 = &dup
	members[1].VirtualIPv4 = &dup
	if _, err := central.Compile(network, members, nil, nil); err == nil {
		t.Fatal("expected duplicate address error")
	}
}

func TestCompileReportsExhaustion(t *testing.T) {
	network, members := fixture()
	small := "10.42.0.0/30"
	network.VirtualCIDR = &small
	if _, err := central.Compile(network, members, nil, nil); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestCompileBuildsACLChain(t *testing.T) {
	network, members := fixture()
	policy := `{"default_action":"deny","rules":[{"id":"r1","name":"allow-web","enabled":true,"action":"allow",` +
		`"sources":[{"type":"member","member_id":"dev-2"}],` +
		`"destinations":[{"type":"member","member_id":"dev-1"}],` +
		`"protocols":[{"protocol":"tcp","ports":["22"],"stateful":true}]}]}`
	network.ACLPolicy = &policy
	plan, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	compiled := plan.Members[0].Config.GetAcl()
	if compiled == nil || compiled.AclV1 == nil || len(compiled.AclV1.Chains) != 1 {
		t.Fatalf("acl = %+v", compiled)
	}
	chain := compiled.AclV1.Chains[0]
	if chain.ChainType != acl.ChainType_Inbound {
		t.Fatalf("chain type = %v", chain.ChainType)
	}
	if chain.DefaultAction != acl.Action_Drop {
		t.Fatalf("default action = %v", chain.DefaultAction)
	}
	if len(chain.Rules) != 1 {
		t.Fatalf("rules = %d", len(chain.Rules))
	}
	rule := chain.Rules[0]
	if rule.Action != acl.Action_Allow || rule.Protocol != acl.Protocol_TCP {
		t.Fatalf("rule = %+v", rule)
	}
	if len(rule.DestinationIps) != 1 || !strings.HasSuffix(rule.DestinationIps[0], "/32") {
		t.Fatalf("destination ips = %v", rule.DestinationIps)
	}
}

func TestSecureModeDerivesStableKeys(t *testing.T) {
	network, members := fixture()
	network.SecureMode = true
	first, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := central.Compile(network, members, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	a := first.Members[0].Config.GetSecureMode()
	b := second.Members[0].Config.GetSecureMode()
	if a == nil || !a.GetEnabled() {
		t.Fatal("secure mode missing")
	}
	if a.GetLocalPrivateKey() == "" || a.GetLocalPublicKey() == "" {
		t.Fatal("keys missing")
	}
	if a.GetLocalPrivateKey() != b.GetLocalPrivateKey() {
		t.Fatal("keys are not stable across compiles")
	}
	c := first.Members[1].Config.GetSecureMode()
	if c.GetLocalPrivateKey() == a.GetLocalPrivateKey() {
		t.Fatal("different members share a key")
	}
}
