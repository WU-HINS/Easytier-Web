package central

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"

	"golang.org/x/crypto/curve25519"
	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/acl"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
)

// MemberPlan is the compiled configuration of one member.
type MemberPlan struct {
	DeviceID    string
	MemberID    string
	Hostname    string
	VirtualIPv4 string
	InstanceID  string
	Config      *manage.NetworkConfig
}

// Plan is the compiled form of one network intent.
type Plan struct {
	NetworkID string
	Members   []MemberPlan
}

// Compile turns intent plus membership into per-member configurations. It is a
// pure function: no I/O, so it can be unit-tested against golden configs.
func Compile(network bal.Network, members []bal.NetworkMember, devices map[string]bal.Device, credentials []bal.NetworkCredential) (Plan, error) {
	policy, err := ParsePolicy(network.ACLPolicy)
	if err != nil {
		return Plan{}, err
	}
	ordered := append([]bal.NetworkMember(nil), members...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreateTime.Equal(ordered[j].CreateTime) {
			return ordered[i].DeviceID < ordered[j].DeviceID
		}
		return ordered[i].CreateTime.Before(ordered[j].CreateTime)
	})

	ips, prefix, err := addressSpace(network.VirtualCIDR)
	if err != nil {
		return Plan{}, err
	}
	used := map[string]bool{}
	allocated := make(map[string]string, len(ordered))
	for _, member := range ordered {
		if member.VirtualIPv4 != nil && *member.VirtualIPv4 != "" {
			if _, ok := used[*member.VirtualIPv4]; ok {
				return Plan{}, fmt.Errorf("central: duplicate virtual ip %s", *member.VirtualIPv4)
			}
			used[*member.VirtualIPv4] = true
			allocated[member.DeviceID] = *member.VirtualIPv4
		}
	}
	for _, member := range ordered {
		if _, ok := allocated[member.DeviceID]; ok {
			continue
		}
		if member.AllocatedIPv4 != nil && *member.AllocatedIPv4 != "" && !used[*member.AllocatedIPv4] {
			used[*member.AllocatedIPv4] = true
			allocated[member.DeviceID] = *member.AllocatedIPv4
			continue
		}
		candidate, ok := nextFree(ips, used)
		if !ok {
			return Plan{}, fmt.Errorf("central: virtual subnet %s exhausted", *network.VirtualCIDR)
		}
		allocated[member.DeviceID] = candidate
	}

	// Defense in depth for rows written before the API validated addresses: an
	// out of subnet address would produce a config no peer can reach.
	if network.VirtualCIDR != nil {
		_, ipnet, err := net.ParseCIDR(*network.VirtualCIDR)
		if err != nil {
			return Plan{}, fmt.Errorf("central: parse virtual subnet: %w", err)
		}
		for deviceID, address := range allocated {
			parsed := net.ParseIP(address)
			if parsed == nil || !ipnet.Contains(parsed) {
				return Plan{}, fmt.Errorf("central: member %s address %s is outside the network subnet %s", deviceID, address, *network.VirtualCIDR)
			}
		}
	}

	networkOverrides, err := ParseOverrides(network.Advanced)
	if err != nil {
		return Plan{}, fmt.Errorf("central: network advanced settings: %w", err)
	}
	plan := Plan{NetworkID: network.ID}
	for _, member := range ordered {
		memberOverrides, err := ParseOverrides(member.OverrideConfig)
		if err != nil {
			return Plan{}, err
		}
		// Network settings are inherited; the member wins field by field.
		overrides := MergeOverrides(networkOverrides, memberOverrides)
		device := devices[member.DeviceID]
		hostname := device.Hostname
		if member.HostnameOverride != nil && *member.HostnameOverride != "" {
			hostname = *member.HostnameOverride
		}
		if hostname == "" {
			hostname = shortID(member.DeviceID)
		}
		virtualIP := allocated[member.DeviceID]
		cfg, err := buildConfig(network, member, device, overrides, policy, hostname, virtualIP, prefix, allocated, credentials)
		if err != nil {
			return Plan{}, err
		}
		plan.Members = append(plan.Members, MemberPlan{
			DeviceID:    member.DeviceID,
			MemberID:    member.ID,
			Hostname:    hostname,
			VirtualIPv4: virtualIP,
			InstanceID:  cfg.GetInstanceId(),
			Config:      cfg,
		})
	}
	return plan, nil
}

func buildConfig(
	network bal.Network,
	member bal.NetworkMember,
	device bal.Device,
	overrides Overrides,
	policy Policy,
	hostname, virtualIP string,
	prefix int,
	allocated map[string]string,
	credentials []bal.NetworkCredential,
) (*manage.NetworkConfig, error) {
	// A member admitted through an admission credential authenticates with that
	// credential, not with the network secret (upstream: network_secret is only
	// set for non temporary members).
	temporary := member.CredentialID != nil && *member.CredentialID != ""
	method := manage.NetworkingMethod_PublicServer
	peerURLs := append([]string(nil), network.PeerURLs...)
	publicServer := ""
	switch network.NetworkingMethod {
	case "Standalone":
		method = manage.NetworkingMethod_Standalone
	case "Manual":
		method = manage.NetworkingMethod_Manual
	case "Gateway":
		// The control plane reserves the gateway mesh name; members join it as a
		// manual network pointed at the gateway peer.
		method = manage.NetworkingMethod_Manual
		if network.PublicServerURL != nil {
			peerURLs = []string{*network.PublicServerURL}
		}
	default:
		method = manage.NetworkingMethod_PublicServer
		if network.PublicServerURL != nil {
			publicServer = *network.PublicServerURL
		}
	}

	// Upstream sorts and dedups peer urls, so reordering them in the console is
	// not a configuration change and must not trigger a re-push.
	peerURLs = dedupeSorted(peerURLs)

	cfg := &manage.NetworkConfig{
		InstanceId:  proto.String(InstanceID(network.ID, member.DeviceID)),
		Hostname:    proto.String(hostname),
		NetworkName: proto.String(network.NetworkName),

		NetworkingMethod: method.Enum(),
		PeerUrls:         peerURLs,
		VirtualIpv4:      proto.String(virtualIP),
		NetworkLength:    proto.Int32(int32(prefix)),
		ProxyCidrs:       append([]string(nil), overrides.ProxyCIDRs...),
		ListenerUrls:     append([]string(nil), overrides.ListenerURLs...),
	}
	if temporary {
		cfg.NetworkSecret = nil
	} else {
		cfg.NetworkSecret = proto.String(network.NetworkSecret)
	}
	if publicServer != "" {
		cfg.PublicServerUrl = proto.String(publicServer)
	}
	if overrides.NoTun != nil {
		cfg.NoTun = proto.Bool(*overrides.NoTun)
	}
	if overrides.DisableIPv6 != nil {
		cfg.DisableIpv6 = proto.Bool(*overrides.DisableIPv6)
	}
	if overrides.MultiThread != nil {
		cfg.MultiThread = proto.Bool(*overrides.MultiThread)
	}
	if overrides.LatencyFirst != nil {
		cfg.LatencyFirst = proto.Bool(*overrides.LatencyFirst)
	}
	if overrides.DisableP2P != nil {
		cfg.DisableP2P = proto.Bool(*overrides.DisableP2P)
	}
	if overrides.MTU != nil {
		cfg.Mtu = proto.Int32(*overrides.MTU)
	}
	if overrides.InstanceRecvBpsLimit != nil {
		cfg.InstanceRecvBpsLimit = proto.Uint64(*overrides.InstanceRecvBpsLimit)
	}
	if overrides.EnableExitNode != nil {
		cfg.EnableExitNode = proto.Bool(*overrides.EnableExitNode)
	}
	if overrides.DisableEncryption != nil {
		cfg.DisableEncryption = proto.Bool(*overrides.DisableEncryption)
	}
	switch {
	case temporary:
		// The admission credential is the identity; the network secret is not
		// handed out at all. A membership that outlived its credential cannot
		// produce a usable config, so say so instead of shipping an empty key.
		secret := ""
		found := false
		for _, credential := range credentials {
			if credential.CredentialID == *member.CredentialID {
				secret = credential.CredentialSecret
				found = true
				break
			}
		}
		if !found || secret == "" {
			return nil, fmt.Errorf("central: member %s references credential %s which no longer exists",
				member.DeviceID, *member.CredentialID)
		}
		cfg.SecureMode = &common.SecureModeConfig{
			Enabled:         true,
			LocalPrivateKey: proto.String(secret),
		}
	case network.SecureMode:
		private, public, err := secureIdentity(network.NetworkSecret, member.DeviceID)
		if err != nil {
			return nil, err
		}
		cfg.SecureMode = &common.SecureModeConfig{
			Enabled:         true,
			LocalPrivateKey: proto.String(private),
			LocalPublicKey:  proto.String(public),
		}
		// Peers holding one of these credentials may join the secure network.
		cfg.ManagedCredentials = managedCredentials(credentials)
	}
	compiled, err := compileACL(policy, member, allocated)
	if err != nil {
		return nil, err
	}
	if compiled != nil {
		cfg.Acl = compiled
	}
	if err := validateCompiledConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// compileACL maps the console policy onto the inbound chain the node enforces.
func compileACL(policy Policy, member bal.NetworkMember, allocated map[string]string) (*acl.Acl, error) {
	defaultAction := acl.Action_Allow
	if strings.EqualFold(policy.DefaultAction, "deny") {
		defaultAction = acl.Action_Drop
	}
	chain := &acl.Chain{
		Name:          "console-inbound",
		ChainType:     acl.ChainType_Inbound,
		Enabled:       true,
		DefaultAction: defaultAction,
	}
	for index, rule := range policy.Rules {
		if !rule.Enabled {
			continue
		}
		action := acl.Action_Allow
		if strings.EqualFold(rule.Action, "deny") {
			action = acl.Action_Drop
		}
		priority := uint32(65535 - index)
		protocols := rule.Protocols
		if len(protocols) == 0 {
			protocols = []ProtocolTarget{{Protocol: "any"}}
		}
		for _, target := range protocols {
			sourceIPs, err := selectorCIDRs(rule.Sources, allocated)
			if err != nil {
				return nil, err
			}
			destinationIPs, err := selectorCIDRs(rule.Destinations, allocated)
			if err != nil {
				return nil, err
			}
			compiled := &acl.Rule{
				Name:              rule.Name,
				Priority:          priority,
				Enabled:           true,
				Protocol:          protocolEnum(target.Protocol),
				Ports:             target.Ports,
				Stateful:          target.Stateful,
				Action:            action,
				SourceIps:         sourceIPs,
				DestinationIps:    destinationIPs,
				SourceGroups:      selectorGroups(rule.Sources),
				DestinationGroups: selectorGroups(rule.Destinations),
			}
			chain.Rules = append(chain.Rules, compiled)
		}
	}
	return &acl.Acl{AclV1: &acl.AclV1{
		Chains: []*acl.Chain{chain},
		Group:  &acl.GroupInfo{},
	}}, nil
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := append([]string(nil), values...)
	sort.Strings(out)
	deduped := out[:1]
	for _, value := range out[1:] {
		if value != deduped[len(deduped)-1] {
			deduped = append(deduped, value)
		}
	}
	return deduped
}

func protocolEnum(name string) acl.Protocol {
	switch strings.ToLower(name) {
	case "tcp":
		return acl.Protocol_TCP
	case "udp":
		return acl.Protocol_UDP
	case "icmp":
		return acl.Protocol_ICMP
	case "icmpv6":
		return acl.Protocol_ICMPv6
	default:
		return acl.Protocol_Any
	}
}

// selectorCIDRs turns selectors into address constraints. An unknown selector
// type is an error on purpose: silently dropping it widens the rule to "any
// address", which turns a narrow allow rule into an allow-all rule.
func selectorCIDRs(selectors []Selector, allocated map[string]string) ([]string, error) {
	var out []string
	for _, selector := range selectors {
		switch selector.Type {
		case "all", "":
			return nil, nil
		case "member":
			if ip, ok := allocated[selector.MemberID]; ok {
				out = append(out, ip+"/32")
			} else {
				return nil, fmt.Errorf("central: acl selector references unknown member %q", selector.MemberID)
			}
		case "group":
			// Groups are matched by the group fields, not by address.
			continue
		case "subnet":
			for _, cidr := range selector.CIDRs {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return nil, fmt.Errorf("central: acl selector cidr %q is invalid", cidr)
				}
				out = append(out, cidr)
			}
		default:
			return nil, fmt.Errorf("central: unknown acl selector type %q", selector.Type)
		}
	}
	return out, nil
}

func selectorGroups(selectors []Selector) []string {
	var out []string
	for _, selector := range selectors {
		if selector.Type == "group" && selector.Name != "" {
			out = append(out, selector.Name)
		}
	}
	return out
}

// addressSpace expands a virtual CIDR into the assignable host addresses.
func addressSpace(cidr *string) ([]string, int, error) {
	if cidr == nil || *cidr == "" {
		return nil, 0, fmt.Errorf("central: virtual subnet is required")
	}
	ip, network, err := net.ParseCIDR(*cidr)
	if err != nil {
		return nil, 0, fmt.Errorf("central: parse virtual subnet: %w", err)
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return nil, 0, fmt.Errorf("central: virtual subnet must be IPv4, got %s", *cidr)
	}
	prefix, bits := network.Mask.Size()
	if bits != 32 {
		return nil, 0, fmt.Errorf("central: virtual subnet must be IPv4")
	}
	base := uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
	size := uint32(1) << (32 - prefix)
	var hosts []string
	for offset := uint32(1); offset < size-1; offset++ {
		value := base + offset
		hosts = append(hosts, fmt.Sprintf("%d.%d.%d.%d", byte(value>>24), byte(value>>16), byte(value>>8), byte(value)))
	}
	if len(hosts) == 0 {
		return nil, 0, fmt.Errorf("central: virtual subnet %s has no usable host", *cidr)
	}
	return hosts, prefix, nil
}

func nextFree(hosts []string, used map[string]bool) (string, bool) {
	for _, host := range hosts {
		if !used[host] {
			used[host] = true
			return host, true
		}
	}
	return "", false
}

// instanceID derives a stable instance UUID from the network and device, so a
// recompiled intent updates the same runtime instance instead of adding one.
// managedCredentials hands the secure network the admission credentials it has
// to accept, matching upstream compile_managed_credential.
func managedCredentials(credentials []bal.NetworkCredential) []*manage.ManagedCredentialConfig {
	if len(credentials) == 0 {
		return nil
	}
	out := make([]*manage.ManagedCredentialConfig, 0, len(credentials))
	for _, credential := range credentials {
		out = append(out, &manage.ManagedCredentialConfig{
			CredentialId:      credential.CredentialID,
			CredentialSecret:  credential.CredentialSecret,
			Groups:            credential.ACLGroups,
			AllowRelay:        credential.AllowRelay,
			AllowedProxyCidrs: credential.AllowedProxyCIDRs,
			ExpiryUnix:        credential.ExpiryUnix,
			Reusable:          proto.Bool(credential.Reusable),
		})
	}
	return out
}

// InstanceID is the deterministic instance id the console assigns to one
// network membership. Exported so callers and tests can reason about which
// instances the console owns.
func InstanceID(networkID, deviceID string) string {
	sum := sha256.Sum256([]byte("easytier-console/instance/" + networkID + "/" + deviceID))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

// secureIdentity derives the stable X25519 identity a member presents in secure
// mode. Migration note: these keys differ from the 2.7.0 Rust derivation, so
// enabling secure mode after the rewrite rotates every member key once.
func secureIdentity(networkSecret, deviceID string) (private, public string, err error) {
	seed := sha256.Sum256([]byte("easytier-console/secure/" + deviceID + "/" + networkSecret))
	var key [32]byte
	copy(key[:], seed[:])
	pub, err := curve25519.X25519(key[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("central: derive secure identity: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key[:]), base64.StdEncoding.EncodeToString(pub), nil
}

func shortID(value string) string {
	if len(value) >= 8 {
		return value[:8]
	}
	return value
}
