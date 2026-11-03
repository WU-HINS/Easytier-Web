package httpapi

import (
	"fmt"
	"net/url"
	"strings"
)

// validateSettingPatch is the authoritative contract for admin settings. The
// client validates the same rules for UX, but a client is not trusted: every
// bound below is enforced here as well.
func validateSettingPatch(patch map[string]any) error {
	for field, raw := range patch {
		switch field {
		case "max_instances_per_user":
			value := toInt(raw)
			if value < 0 || value > 10000 {
				return fmt.Errorf("max_instances_per_user must be between 0 and 10000")
			}
		case "password_min_length":
			value := toInt(raw)
			if value < 6 || value > 128 {
				return fmt.Errorf("password_min_length must be between 6 and 128")
			}
		case "password_min_classes":
			value := toInt(raw)
			if value < 1 || value > 4 {
				return fmt.Errorf("password_min_classes must be between 1 and 4")
			}
		case "reserved_usernames":
			for _, name := range strings.Split(toString(raw), ",") {
				trimmed := strings.TrimSpace(name)
				if trimmed == "" {
					continue
				}
				if len(trimmed) > 32 {
					return fmt.Errorf("reserved username %q is longer than 32 characters", trimmed)
				}
				for _, r := range trimmed {
					valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
						(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
					if !valid {
						return fmt.Errorf("reserved username %q contains an invalid character", trimmed)
					}
				}
			}
		case "extra_port":
			value := toInt(raw)
			if value != 0 && (value < 1024 || value > 65535) {
				return fmt.Errorf("extra_port must be 0 or between 1024 and 65535")
			}
		case "oidc_issuer":
			value := strings.TrimSpace(toString(raw))
			if value == "" {
				continue
			}
			parsed, err := url.Parse(value)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return fmt.Errorf("oidc_issuer must be an absolute http(s) URL")
			}
		case "expected_revision":
			// Optimistic concurrency token, validated by the data layer.
		case "oidc_enabled", "registration_open", "extra_tcp_enabled", "extra_udp_enabled",
			"oidc_client_id", "oidc_client_secret", "oidc_button_label", "background_url":
			// Type level validation happens in the handler; nothing else to bound.
		default:
			return fmt.Errorf("unknown setting: %s", field)
		}
	}
	return nil
}

// validateSettingsState enforces the cross field rules after the patch is known:
// enabling OIDC requires an issuer and a client id, and a production issuer must
// be https unless the operator explicitly allowed insecure mode.
func validateSettingsState(oidcEnabled bool, issuer, clientID string, allowInsecure bool) error {
	if !oidcEnabled {
		return nil
	}
	if issuer == "" {
		return fmt.Errorf("oidc_issuer is required when OIDC is enabled")
	}
	if clientID == "" {
		return fmt.Errorf("oidc_client_id is required when OIDC is enabled")
	}
	if !allowInsecure && !strings.HasPrefix(issuer, "https://") {
		return fmt.Errorf("oidc_issuer must use https (enable oidc.allow_insecure for development)")
	}
	return nil
}
