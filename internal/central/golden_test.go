package central_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/prototext"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// goldenScenario is one fixed intent. The compiled output is compared against a
// committed file so any field drift shows up as a diff instead of slipping
// through per-field assertions that nobody updates.
type goldenScenario struct {
	name        string
	network     bal.Network
	members     []bal.NetworkMember
	devices     map[string]bal.Device
	credentials []bal.NetworkCredential
}

func strPtr(value string) *string { return &value }

func scenarios() []goldenScenario {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cidr := "10.42.0.0/29"
	base := bal.Network{
		ID: "11111111-1111-4111-8111-111111111111", NetworkName: "golden-net",
		NetworkSecret: "golden-secret", NetworkingMethod: "Standalone",
		VirtualCIDR: &cidr, CreateTime: now, UpdateTime: now,
	}
	devices := map[string]bal.Device{
		"22222222-2222-4222-8222-222222222222": {MachineID: "22222222-2222-4222-8222-222222222222", Hostname: "alpha"},
		"33333333-3333-4333-8333-333333333333": {MachineID: "33333333-3333-4333-8333-333333333333", Hostname: "beta"},
	}
	memberA := bal.NetworkMember{
		ID: "aaaaaaaa-1111-4111-8111-111111111111", NetworkID: base.ID,
		DeviceID: "22222222-2222-4222-8222-222222222222", CreateTime: now, UpdateTime: now,
	}
	memberB := bal.NetworkMember{
		ID: "bbbbbbbb-1111-4111-8111-111111111111", NetworkID: base.ID,
		DeviceID:   "33333333-3333-4333-8333-333333333333",
		CreateTime: now.Add(time.Second), UpdateTime: now,
	}

	secure := base
	secure.SecureMode = true

	manual := base
	manual.NetworkingMethod = "Manual"
	manual.PeerURLs = []string{"tcp://relay.example.com:11010"}

	acl := base
	policy := `{"default_action":"deny","rules":[{"id":"allow-subnet","action":"allow","enabled":true,"sources":[{"type":"subnet","cidrs":["10.42.0.0/29"]}]}]}`
	acl.ACLPolicy = &policy

	override := base
	overrideJSON := `{"no_tun":true,"mtu":1400,"proxy_cidrs":["192.168.10.0/24"]}`
	memberOverride := memberB
	memberOverride.OverrideConfig = &overrideJSON

	hostnameOverride := memberB
	hostnameOverride.HostnameOverride = strPtr("beta-renamed")

	temporary := memberB
	temporary.CredentialID = strPtr("cred-1")

	return []goldenScenario{
		{name: "standalone-plain", network: base, members: []bal.NetworkMember{memberA, memberB}, devices: devices},
		{name: "standalone-secure", network: secure, members: []bal.NetworkMember{memberA}, devices: devices},
		{name: "manual-with-peers", network: manual, members: []bal.NetworkMember{memberA}, devices: devices},
		{name: "acl-policy", network: acl, members: []bal.NetworkMember{memberA}, devices: devices},
		{name: "member-override", network: override, members: []bal.NetworkMember{memberA, memberOverride}, devices: devices},
		{name: "hostname-override", network: base, members: []bal.NetworkMember{memberA, hostnameOverride}, devices: devices},
		{name: "temporary-credential", network: secure, members: []bal.NetworkMember{memberA, temporary}, devices: devices,
			credentials: []bal.NetworkCredential{{
				UserID: 1, NetworkID: base.ID, CredentialID: "cred-1", CredentialSecret: "credential-secret",
				ExpiryUnix: 1893456000, ACLGroups: []string{"ops"}, AllowRelay: true, Reusable: true,
			}}},
	}
}

func renderGolden(t *testing.T, scenario goldenScenario) string {
	t.Helper()
	plan, err := central.Compile(scenario.network, scenario.members, scenario.devices, scenario.credentials)
	if err != nil {
		t.Fatalf("%s: compile: %v", scenario.name, err)
	}
	marshal := prototext.MarshalOptions{Multiline: true, Indent: "  "}
	out := ""
	for _, member := range plan.Members {
		rendered, err := marshal.Marshal(member.Config)
		if err != nil {
			t.Fatalf("%s: marshal: %v", scenario.name, err)
		}
		out += "# device " + member.DeviceID + "\n" +
			"# hostname " + member.Hostname + "\n" +
			"# virtual_ipv4 " + member.VirtualIPv4 + "\n" +
			string(rendered) + "\n"
	}
	return normaliseWhitespace(out)
}

// normaliseWhitespace collapses the space run after a colon so the golden
// files record semantics, not the text marshaller's spacing preference. A one
// space vs two space difference is noise; a changed token is a real drift.
func normaliseWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		for strings.Contains(line, ":  ") {
			line = strings.ReplaceAll(line, ":  ", ": ")
		}
		lines[index] = line
	}
	return strings.Join(lines, "\n")
}

// TestCompiledConfigsMatchGolden pins the whole compiled configuration. Update
// with UPDATE_GOLDEN=1 and review the diff before committing it.
func TestCompiledConfigsMatchGolden(t *testing.T) {
	root := filepath.Join("testdata", "golden")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create golden dir: %v", err)
	}
	update := os.Getenv("UPDATE_GOLDEN") == "1"

	for _, scenario := range scenarios() {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			got := renderGolden(t, scenario)
			path := filepath.Join(root, scenario.name+".txt")
			if update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
			}
			if string(want) != got {
				t.Fatalf("compiled config changed for %s\nwant:\n%s\ngot:\n%s", scenario.name, want, got)
			}
		})
	}
}
