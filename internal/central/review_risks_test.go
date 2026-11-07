package central_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// F-066: the console derives the instance id from (network, device) instead of
// generating one per start like upstream does. That divergence was accepted on
// the grounds that the protocol only needs "one identifier per instance" and the
// control plane needs a stable one. This test states both properties.
func TestInstanceIDIsDeterministicAndVersionFour(t *testing.T) {
	first := central.InstanceID("network-1", "device-1")
	second := central.InstanceID("network-1", "device-1")
	if first != second {
		t.Fatalf("the same membership produced two ids: %s and %s", first, second)
	}
	if central.InstanceID("network-1", "device-2") == first {
		t.Fatal("two devices in one network must not share an instance id")
	}
	if central.InstanceID("network-2", "device-1") == first {
		t.Fatal("the same device in two networks must not share an instance id")
	}
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidV4.MatchString(first) {
		t.Fatalf("%s is not a version 4 RFC 4122 uuid", first)
	}
}

// F-073: compile errors are the only explanation an operator gets when a network
// stops updating, so each one has to name the field or value that is wrong.
func TestCompileErrorsNameTheField(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name:   "unknown acl selector type",
			policy: `{"default_action":"allow","rules":[{"id":"r1","name":"r1","enabled":true,"action":"allow","sources":[{"type":"cidr","cidrs":["10.0.0.0/8"]}]}]}`,
			want:   "unknown acl selector type",
		},
		{
			name:   "invalid acl subnet",
			policy: `{"default_action":"allow","rules":[{"id":"r1","name":"r1","enabled":true,"action":"allow","sources":[{"type":"subnet","cidrs":["not-a-cidr"]}]}]}`,
			want:   "not-a-cidr",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			network := bal.Network{
				ID: "11111111-1111-4111-8111-111111111111", DisplayName: "errors",
				NetworkName: "errors", NetworkSecret: "secret", NetworkingMethod: "Standalone",
				VirtualCIDR: stringPtr("10.30.0.0/29"), ACLPolicy: stringPtr(testCase.policy),
			}
			members := []bal.NetworkMember{{NetworkID: network.ID, DeviceID: "22222222-2222-4222-8222-222222222222"}}
			_, err := central.Compile(network, members, map[string]bal.Device{}, nil)
			if err == nil {
				t.Fatal("a broken policy compiled without an error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error %q does not name %q", err.Error(), testCase.want)
			}
		})
	}
}

// F-081: the fields the console never sets (dhcp, vpn portal, relay whitelist)
// have to stay unset, because "unset" is how the client applies its own default.
// Setting them to false would be a different instruction.
func TestCompiledConfigLeavesOptionalFeaturesUnset(t *testing.T) {
	network := bal.Network{
		ID: "11111111-1111-4111-8111-111111111111", DisplayName: "defaults",
		NetworkName: "defaults", NetworkSecret: "secret", NetworkingMethod: "Standalone",
		VirtualCIDR: stringPtr("10.31.0.0/29"),
	}
	members := []bal.NetworkMember{{NetworkID: network.ID, DeviceID: "22222222-2222-4222-8222-222222222222"}}
	plan, err := central.Compile(network, members, map[string]bal.Device{}, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cfg := plan.Members[0].Config
	if cfg.Dhcp != nil {
		t.Fatalf("dhcp was set to %v; leaving it unset is what makes the client default apply", cfg.GetDhcp())
	}
	if cfg.EnableVpnPortal != nil || cfg.VpnPortalListenPort != nil || cfg.VpnPortalClientNetworkAddr != nil {
		t.Fatal("the vpn portal fields were set although the console never enables the portal")
	}
	if cfg.EnableRelayNetworkWhitelist != nil {
		t.Fatal("the relay network whitelist switch was set although the console does not manage it")
	}
}

func stringPtr(value string) *string { return &value }
