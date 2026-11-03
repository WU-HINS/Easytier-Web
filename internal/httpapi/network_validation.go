package httpapi

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// validateNetworkIntent rejects intents that the compile step cannot turn into a
// configuration. Storing them is worse than rejecting them: compile failures
// stop every push for the tenant and only show up as a log line.
func validateNetworkIntent(network bal.Network) error {
	if network.VirtualCIDR == nil || strings.TrimSpace(*network.VirtualCIDR) == "" {
		return fmt.Errorf("virtual_cidr is required")
	}
	cidr := strings.TrimSpace(*network.VirtualCIDR)
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("virtual_cidr %q is not a valid CIDR", cidr)
	}
	if ip.To4() == nil || ipnet.IP.To4() == nil {
		return fmt.Errorf("virtual_cidr must be IPv4, got %q", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return fmt.Errorf("virtual_cidr must be IPv4, got %q", cidr)
	}
	if ones < 8 || ones > 30 {
		return fmt.Errorf("virtual_cidr prefix must be between /8 and /30, got /%d", ones)
	}

	switch network.NetworkingMethod {
	case "Standalone":
		return nil
	case "PublicServer":
		// The core turns this into the only peer it will dial: an empty value
		// fails to parse and the instance never starts.
		if network.PublicServerURL == nil || strings.TrimSpace(*network.PublicServerURL) == "" {
			return fmt.Errorf("networking_method PublicServer requires public_server_url")
		}
		if err := validateEndpointURL(*network.PublicServerURL); err != nil {
			return fmt.Errorf("public_server_url: %w", err)
		}
		return nil
	case "Manual":
		// Upstream rejects an empty peer list for Manual (EmptyPeerUrls); a
		// public server URL is not an alternative there.
		if len(network.PeerURLs) == 0 {
			return fmt.Errorf("networking_method Manual requires peer_urls")
		}
		for _, peer := range network.PeerURLs {
			if err := validateEndpointURL(peer); err != nil {
				return fmt.Errorf("peer_urls: %w", err)
			}
		}
		return nil
	case "Gateway":
		// A gateway mesh is defined by the peer it joins.
		if network.PublicServerURL == nil || strings.TrimSpace(*network.PublicServerURL) == "" {
			return fmt.Errorf("networking_method Gateway requires public_server_url")
		}
		if err := validateEndpointURL(*network.PublicServerURL); err != nil {
			return fmt.Errorf("public_server_url: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown networking_method %q", network.NetworkingMethod)
	}
}

// validateEndpointURL accepts the schemes easytier understands and requires a
// host, so a typo like "tcp//1.2.3.4:11010" is caught at write time.
func validateEndpointURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%q is not a valid URL", raw)
	}
	switch parsed.Scheme {
	case "tcp", "udp", "ws", "wss", "http", "https":
	default:
		return fmt.Errorf("%q must start with tcp://, udp://, ws:// or wss://", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%q is missing a host", raw)
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err != nil {
		return fmt.Errorf("%q must include a port", raw)
	}
	return nil
}

// validateMemberAddress keeps a member address inside the network subnet;
// compile would happily emit an out-of-subnet address that no peer can reach.
func validateMemberAddress(cidr *string, address string) error {
	if strings.TrimSpace(address) == "" {
		return nil
	}
	if cidr == nil || strings.TrimSpace(*cidr) == "" {
		return fmt.Errorf("network has no virtual subnet to place %q in", address)
	}
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(*cidr))
	if err != nil {
		return fmt.Errorf("network virtual_cidr is invalid")
	}
	parsed := net.ParseIP(strings.TrimSpace(address))
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("%q is not an IPv4 address", address)
	}
	if !ipnet.Contains(parsed) {
		return fmt.Errorf("%q is outside the network subnet %s", address, *cidr)
	}
	return nil
}
