package central_test

import (
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestCompileMatchesUpstreamCredentialRules covers F-065: upstream (compiler.rs)
// only ships network_secret to non temporary members and hands the secure
// network the list of admission credentials it must accept. Our compiler did
// neither, so secure mode ignored credentials entirely.
func TestCompileMatchesUpstreamCredentialRules(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.40.0.0/29"
	secret := func(value string) *string { return &value }
	network := bal.Network{
		ID: "net-cred", NetworkName: "cred-net", NetworkSecret: "network-secret",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr, SecureMode: true,
		CreateTime: now, UpdateTime: now,
	}
	deviceMember := bal.NetworkMember{
		ID: "m1", NetworkID: "net-cred", DeviceID: "dev-1", CreateTime: now, UpdateTime: now,
	}
	temporaryMember := bal.NetworkMember{
		ID: "m2", NetworkID: "net-cred", DeviceID: "dev-2", CredentialID: secret("cred-1"),
		CreateTime: now.Add(time.Second), UpdateTime: now,
	}
	devices := map[string]bal.Device{
		"dev-1": {MachineID: "dev-1", Hostname: "a"},
		"dev-2": {MachineID: "dev-2", Hostname: "b"},
	}
	credentials := []bal.NetworkCredential{{
		UserID: 1, NetworkID: "net-cred", CredentialID: "cred-1",
		CredentialSecret: "credential-secret", ExpiryUnix: 1234,
		ACLGroups: []string{"ops"}, AllowRelay: true, Reusable: true,
	}}

	plan, err := central.Compile(network, []bal.NetworkMember{deviceMember, temporaryMember}, devices, credentials)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(plan.Members) != 2 {
		t.Fatalf("members = %d", len(plan.Members))
	}

	byDevice := map[string]int{}
	for index, member := range plan.Members {
		byDevice[member.DeviceID] = index
	}
	normal := plan.Members[byDevice["dev-1"]].Config
	if normal.GetNetworkSecret() != "network-secret" {
		t.Fatalf("normal member must receive the network secret, got %q", normal.GetNetworkSecret())
	}
	if got := len(normal.GetManagedCredentials()); got != 1 {
		t.Fatalf("managed credentials = %d, want 1", got)
	}
	if normal.GetManagedCredentials()[0].GetCredentialId() != "cred-1" {
		t.Fatalf("managed credential id = %q", normal.GetManagedCredentials()[0].GetCredentialId())
	}
	if !normal.GetSecureMode().GetEnabled() || normal.GetSecureMode().GetLocalPrivateKey() == "" {
		t.Fatalf("secure mode not configured for the normal member")
	}

	temporary := plan.Members[byDevice["dev-2"]].Config
	if temporary.GetNetworkSecret() != "" {
		t.Fatalf("temporary member must not receive the network secret, got %q", temporary.GetNetworkSecret())
	}
	if temporary.GetSecureMode().GetLocalPrivateKey() != "credential-secret" {
		t.Fatalf("temporary member key = %q, want the credential secret", temporary.GetSecureMode().GetLocalPrivateKey())
	}
	if got := len(temporary.GetManagedCredentials()); got != 0 {
		t.Fatalf("temporary member got %d managed credentials, want 0", got)
	}
}

// TestSecureModeMatrix pins the four combinations of secure mode and admission
// credential against upstream rules (compiler.rs + the core's invariant that
// only nodes holding a network_secret may declare managed credentials).
func TestSecureModeMatrix(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.45.0.0/29"
	credential := bal.NetworkCredential{
		UserID: 1, NetworkID: "net-matrix", CredentialID: "cred-1",
		CredentialSecret: "credential-secret", Reusable: true,
	}
	credentialID := "cred-1"

	cases := []struct {
		name              string
		secure            bool
		temporary         bool
		wantSecureMode    bool
		wantLocalKey      string
		wantCredentials   int
		wantNetworkSecret bool
	}{
		{name: "plain", wantSecureMode: false, wantCredentials: 0, wantNetworkSecret: true},
		{name: "secure", secure: true, wantSecureMode: true, wantCredentials: 1, wantNetworkSecret: true},
		{name: "secure+temporary", secure: true, temporary: true, wantSecureMode: true,
			wantLocalKey: "credential-secret", wantCredentials: 0, wantNetworkSecret: false},
		{name: "plain+temporary", temporary: true, wantSecureMode: true,
			wantLocalKey: "credential-secret", wantCredentials: 0, wantNetworkSecret: false},
	}

	for _, testCase := range cases {
		network := bal.Network{
			ID: "net-matrix", NetworkName: "matrix-net", NetworkSecret: "network-secret",
			NetworkingMethod: "Standalone", VirtualCIDR: &cidr, SecureMode: testCase.secure,
			CreateTime: now, UpdateTime: now,
		}
		member := bal.NetworkMember{
			ID: "m1", NetworkID: "net-matrix", DeviceID: "dev-1", CreateTime: now, UpdateTime: now,
		}
		if testCase.temporary {
			member.CredentialID = &credentialID
		}
		plan, err := central.Compile(network, []bal.NetworkMember{member},
			map[string]bal.Device{"dev-1": {MachineID: "dev-1", Hostname: "node"}},
			[]bal.NetworkCredential{credential})
		if err != nil {
			t.Fatalf("%s: compile: %v", testCase.name, err)
		}
		cfg := plan.Members[0].Config

		if got := cfg.GetSecureMode().GetEnabled(); got != testCase.wantSecureMode {
			t.Fatalf("%s: secure mode enabled = %v, want %v", testCase.name, got, testCase.wantSecureMode)
		}
		if testCase.wantLocalKey != "" && cfg.GetSecureMode().GetLocalPrivateKey() != testCase.wantLocalKey {
			t.Fatalf("%s: local key = %q, want the credential secret", testCase.name, cfg.GetSecureMode().GetLocalPrivateKey())
		}
		if got := len(cfg.GetManagedCredentials()); got != testCase.wantCredentials {
			t.Fatalf("%s: managed credentials = %d, want %d", testCase.name, got, testCase.wantCredentials)
		}
		if got := cfg.GetNetworkSecret() != ""; got != testCase.wantNetworkSecret {
			t.Fatalf("%s: network secret present = %v, want %v", testCase.name, got, testCase.wantNetworkSecret)
		}
		// The core rejects managed credentials without a network secret.
		if len(cfg.GetManagedCredentials()) > 0 && cfg.GetNetworkSecret() == "" {
			t.Fatalf("%s: managed credentials without a network secret would be rejected by the core", testCase.name)
		}
	}
}

// TestDanglingCredentialIsReported covers F-083: revoking a credential leaves
// memberships pointing at it. Those members used to compile into a config with
// an empty secure-mode key, which the core refuses with a cryptic error.
func TestDanglingCredentialIsReported(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.46.0.0/29"
	credentialID := "cred-gone"
	network := bal.Network{
		ID: "net-dangling", NetworkName: "dangling-net", NetworkSecret: "network-secret",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr, SecureMode: true,
		CreateTime: now, UpdateTime: now,
	}
	member := bal.NetworkMember{
		ID: "m1", NetworkID: "net-dangling", DeviceID: "dev-1", CredentialID: &credentialID,
		CreateTime: now, UpdateTime: now,
	}
	_, err := central.Compile(network, []bal.NetworkMember{member},
		map[string]bal.Device{"dev-1": {MachineID: "dev-1", Hostname: "node"}}, nil)
	if err == nil {
		t.Fatal("compile accepted a member pointing at a deleted credential")
	}
	if !strings.Contains(err.Error(), "cred-gone") || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("error = %v, want it to name the missing credential", err)
	}
}
