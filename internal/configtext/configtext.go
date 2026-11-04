// Package configtext converts between the console network config and the TOML
// text an operator edits by hand.
//
// It exists so the two HTTP endpoints the original console exposes
// (generate-config and parse-config) do not need a second copy of the field
// definitions: this package owns the mapping, the handlers only translate shapes.
package configtext

import (
	"strings"

	"github.com/BurntSushi/toml"
	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/api/manage"
)

// Document is the TOML shape: the subset of the instance config this console
// writes and reads. Fields it does not model are dropped, which is why parsing
// reports what it ignored rather than silently losing it.
type Document struct {
	InstanceName string   `toml:"instance_name,omitempty"`
	Hostname     string   `toml:"hostname,omitempty"`
	IPv4         string   `toml:"ipv4,omitempty"`
	MTU          int32    `toml:"mtu,omitempty"`
	NoTun        bool     `toml:"no_tun,omitempty"`
	Listeners    []string `toml:"listeners,omitempty"`
	ExitNodes    []string `toml:"exit_nodes,omitempty"`
	NetworkName  string   `toml:"network_name,omitempty"`
	Secret       string   `toml:"network_secret,omitempty"`
	Peers        []Peer   `toml:"peer,omitempty"`
	ProxyNets    []Proxy  `toml:"proxy_network,omitempty"`
}

// Peer is one entry of the peer list.
type Peer struct {
	URI string `toml:"uri"`
}

// Proxy is one entry of the proxied subnet list.
type Proxy struct {
	CIDR string `toml:"cidr"`
}

// FromNetworkConfig maps a pushed config into the document shape.
func FromNetworkConfig(cfg *manage.NetworkConfig) Document {
	doc := Document{
		InstanceName: cfg.GetHostname(),
		Hostname:     cfg.GetHostname(),
		IPv4:         cfg.GetVirtualIpv4(),
		MTU:          cfg.GetMtu(),
		NoTun:        cfg.GetNoTun(),
		Listeners:    append([]string(nil), cfg.GetListenerUrls()...),
		ExitNodes:    append([]string(nil), cfg.GetExitNodes()...),
		NetworkName:  cfg.GetNetworkName(),
		Secret:       cfg.GetNetworkSecret(),
	}
	for _, uri := range cfg.GetPeerUrls() {
		if uri != "" {
			doc.Peers = append(doc.Peers, Peer{URI: uri})
		}
	}
	for _, cidr := range cfg.GetProxyCidrs() {
		if cidr != "" {
			doc.ProxyNets = append(doc.ProxyNets, Proxy{CIDR: cidr})
		}
	}
	return doc
}

// ToNetworkConfig builds a pushed config from the document shape, filling the
// same fields the compiler fills so a round trip through TOML keeps them.
func (d Document) ToNetworkConfig() *manage.NetworkConfig {
	cfg := &manage.NetworkConfig{
		Hostname:      proto.String(d.Hostname),
		VirtualIpv4:   proto.String(d.IPv4),
		NetworkName:   proto.String(d.NetworkName),
		NetworkSecret: proto.String(d.Secret),
		NoTun:         proto.Bool(d.NoTun),
	}
	if d.MTU != 0 {
		cfg.Mtu = proto.Int32(d.MTU)
	}
	cfg.ListenerUrls = append([]string(nil), d.Listeners...)
	cfg.ExitNodes = append([]string(nil), d.ExitNodes...)
	for _, peer := range d.Peers {
		if strings.TrimSpace(peer.URI) != "" {
			cfg.PeerUrls = append(cfg.PeerUrls, strings.TrimSpace(peer.URI))
		}
	}
	for _, proxy := range d.ProxyNets {
		if strings.TrimSpace(proxy.CIDR) != "" {
			cfg.ProxyCidrs = append(cfg.ProxyCidrs, strings.TrimSpace(proxy.CIDR))
		}
	}
	return cfg
}

// Dump renders the document as TOML.
func (d Document) Dump() (string, error) {
	var out strings.Builder
	if err := toml.NewEncoder(&out).Encode(d); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Parse reads TOML into the document shape.
func Parse(text string) (Document, error) {
	var doc Document
	if _, err := toml.Decode(text, &doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}
