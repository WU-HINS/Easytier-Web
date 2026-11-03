package central

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
)

// validateCompiledConfig is the Go counterpart of upstream validate_member_config:
// the config is checked before it is pushed so a broken combination fails here
// (visible on the network page) instead of on the device, where the only trace
// is an instance that refuses to start.
func validateCompiledConfig(cfg *manage.NetworkConfig) error {
	if cfg == nil {
		return fmt.Errorf("central: empty config")
	}
	if strings.TrimSpace(cfg.GetNetworkName()) == "" {
		return fmt.Errorf("central: network name is empty")
	}
	if strings.TrimSpace(cfg.GetInstanceId()) == "" {
		return fmt.Errorf("central: instance id is empty")
	}
	if !looksLikeUUID(cfg.GetInstanceId()) {
		return fmt.Errorf("central: instance id %q is not a uuid", cfg.GetInstanceId())
	}
	if cfg.GetVirtualIpv4() != "" {
		address := net.ParseIP(cfg.GetVirtualIpv4())
		if address == nil || address.To4() == nil {
			return fmt.Errorf("central: virtual ipv4 %q is not an address", cfg.GetVirtualIpv4())
		}
		if prefix := cfg.GetNetworkLength(); prefix < 0 || prefix > 32 {
			return fmt.Errorf("central: network length %d is out of range", prefix)
		}
	}
	for _, cidr := range cfg.GetProxyCidrs() {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("central: proxy cidr %q is invalid", cidr)
		}
	}
	for _, listener := range cfg.GetListenerUrls() {
		if err := validateEndpoint(listener); err != nil {
			return err
		}
	}
	for _, peer := range cfg.GetPeerUrls() {
		if err := validateEndpoint(peer); err != nil {
			return err
		}
	}
	// The core turns PublicServer into a single peer URL; an empty one fails to
	// parse there, so the instance would never start.
	if cfg.GetNetworkingMethod() == manage.NetworkingMethod_PublicServer && cfg.GetPublicServerUrl() == "" {
		return fmt.Errorf("central: PublicServer requires public_server_url")
	}
	if cfg.GetNetworkingMethod() == manage.NetworkingMethod_Manual && len(cfg.GetPeerUrls()) == 0 {
		return fmt.Errorf("central: Manual requires peer_urls")
	}
	if cfg.Mtu != nil {
		if mtu := cfg.GetMtu(); mtu < 576 || mtu > 65535 {
			return fmt.Errorf("central: mtu %d is out of range", mtu)
		}
	}
	return nil
}

func validateEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("central: endpoint %q is not a url", raw)
	}
	switch parsed.Scheme {
	case "tcp", "udp", "ws", "wss", "http", "https":
	default:
		return fmt.Errorf("central: endpoint %q has an unsupported scheme", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("central: endpoint %q is missing a host", raw)
	}
	if _, _, err := net.SplitHostPort(parsed.Host); err != nil {
		return fmt.Errorf("central: endpoint %q must include a port", raw)
	}
	return nil
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		switch index {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
