// Package central ports the 2.7.0 "network" feature: a tenant-scoped intent
// that is compiled into one NetworkConfig per member and pushed to devices
// over the control plane.
package central

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Selector picks the members a rule applies to.
type Selector struct {
	Type     string   `json:"type"`
	MemberID string   `json:"member_id,omitempty"`
	CIDRs    []string `json:"cidrs,omitempty"`
	Name     string   `json:"name,omitempty"`
}

// ProtocolTarget narrows a rule to one protocol and port list.
type ProtocolTarget struct {
	Protocol string   `json:"protocol"`
	Ports    []string `json:"ports,omitempty"`
	Stateful bool     `json:"stateful,omitempty"`
}

// Rule is one ACL rule of a network intent.
type Rule struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Enabled      bool             `json:"enabled"`
	Action       string           `json:"action"`
	Sources      []Selector       `json:"sources,omitempty"`
	Destinations []Selector       `json:"destinations,omitempty"`
	Protocols    []ProtocolTarget `json:"protocols,omitempty"`
}

// Policy is the stored ACL policy of a network and the order-preserving rules
// the compiler turns into an inbound chain.
type Policy struct {
	DefaultAction string `json:"default_action"`
	Rules         []Rule `json:"rules"`
}

// ParsePolicy decodes a stored policy, falling back to allow-all.
func ParsePolicy(raw *string) (Policy, error) {
	if raw == nil || *raw == "" {
		return Policy{DefaultAction: "allow"}, nil
	}
	var policy Policy
	// Unknown keys are rejected: a typo such as "source" instead of "sources"
	// would otherwise be dropped silently and change the effective policy.
	decoder := json.NewDecoder(strings.NewReader(*raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("central: decode acl policy: %w", err)
	}
	if policy.DefaultAction == "" {
		policy.DefaultAction = "allow"
	}
	return policy, nil
}

// Overrides is the per-member escape hatch stored as JSON on the membership.
type Overrides struct {
	NoTun             *bool    `json:"no_tun,omitempty"`
	DisableIPv6       *bool    `json:"disable_ipv6,omitempty"`
	MultiThread       *bool    `json:"multi_thread,omitempty"`
	LatencyFirst      *bool    `json:"latency_first,omitempty"`
	ProxyCIDRs        []string `json:"proxy_cidrs,omitempty"`
	ListenerURLs      []string `json:"listener_urls,omitempty"`
	DisableP2P        *bool    `json:"disable_p2p,omitempty"`
	MTU               *int32   `json:"mtu,omitempty"`
	EnableExitNode    *bool    `json:"enable_exit_node,omitempty"`
	DisableEncryption *bool    `json:"disable_encryption,omitempty"`
}

// ParseOverrides decodes a member override blob.
func ParseOverrides(raw *string) (Overrides, error) {
	if raw == nil || *raw == "" {
		return Overrides{}, nil
	}
	var o Overrides
	decoder := json.NewDecoder(strings.NewReader(*raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&o); err != nil {
		return Overrides{}, fmt.Errorf("central: decode overrides: %w", err)
	}
	return o, nil
}
