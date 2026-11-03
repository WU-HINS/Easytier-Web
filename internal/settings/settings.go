// Package settings is the typed view of the admin-editable configuration that
// lives in the settings table. Environment variables only bootstrap values that
// do not exist yet; the database is authoritative afterwards.
package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// Keys of the settings table.
const (
	KeyInitialized       = "system.initialized"
	KeyMaxInstances      = "quota.max_instances_per_user"
	KeyPasswordMinLength = "security.password_min_length"
	KeyPasswordClasses   = "security.password_min_classes"
	KeyReservedNames     = "security.reserved_usernames"
	KeyBackgroundURL     = "ui.background_url"
	KeyOIDCEnabled       = "oidc.enabled"
	KeyOIDCIssuer        = "oidc.issuer"
	KeyOIDCClientID      = "oidc.client_id"
	KeyOIDCClientSecret  = "oidc.client_secret"
	KeyOIDCButtonLabel   = "oidc.button_label"
	KeyOIDCScopes        = "oidc.scopes"
	KeyExtraTCPEnabled   = "listener.tcp_enabled"
	KeyExtraUDPEnabled   = "listener.udp_enabled"
	KeyExtraPort         = "listener.extra_port"
	KeyRegistrationOpen  = "security.registration_open"
	// KeyTrustProxy enables reading X-Forwarded-For; set it only behind a proxy you control.
	KeyTrustProxy = "server.trust_proxy"
)

// DefaultReserved are always refused as usernames.
const DefaultReserved = "admin,root,system,support,api,www,sso,noreply,security"

// Settings wraps the store with typed accessors.
type Settings struct {
	store *bal.Store
}

// New binds the settings view to a store.
func New(store *bal.Store) *Settings { return &Settings{store: store} }

// Store exposes the underlying store for handlers that also need other data.
func (s *Settings) Store() *bal.Store { return s.store }

// Initialized reports whether the first-run setup has completed.
func (s *Settings) Initialized(ctx context.Context) (bool, error) {
	value, err := s.store.GetSetting(ctx, KeyInitialized)
	if err != nil && err != bal.ErrNotFound {
		return false, err
	}
	if value == "true" {
		return true, nil
	}
	// Self-heal: an admin row implies the system is initialized even if the
	// flag write was lost.
	admins, err := s.store.CountUsersByRole(ctx, bal.RoleAdmin)
	if err != nil {
		return false, err
	}
	return admins > 0, nil
}

// MarkInitialized records completion of the setup flow.
func (s *Settings) MarkInitialized(ctx context.Context) error {
	return s.store.PutSetting(ctx, KeyInitialized, "true")
}

// Int reads an integer setting with a fallback.
func (s *Settings) Int(ctx context.Context, key string, fallback int) int {
	value := s.store.GetSettingDefault(ctx, key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

// Bool reads a boolean setting with a fallback.
func (s *Settings) Bool(ctx context.Context, key string, fallback bool) bool {
	switch strings.ToLower(s.store.GetSettingDefault(ctx, key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// String reads a string setting with a fallback.
func (s *Settings) String(ctx context.Context, key, fallback string) string {
	value, err := s.store.GetSetting(ctx, key)
	if err != nil || value == "" {
		return fallback
	}
	return value
}

// MaxInstancesPerUser is the global instance quota (0 means unlimited).
func (s *Settings) MaxInstancesPerUser(ctx context.Context) int {
	return s.Int(ctx, KeyMaxInstances, 5)
}

// ReservedNames returns the normalised reserved username list.
func (s *Settings) ReservedNames(ctx context.Context) []string {
	raw := s.String(ctx, KeyReservedNames, DefaultReserved)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.ToLower(strings.TrimSpace(part))
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// PasswordPolicy is the configurable credential strength.
type PasswordPolicy struct {
	MinLength int
	Classes   int
}

// PasswordPolicy reads the policy (defaults: 10 chars, 3 classes).
func (s *Settings) PasswordPolicy(ctx context.Context) PasswordPolicy {
	return PasswordPolicy{
		MinLength: s.Int(ctx, KeyPasswordMinLength, 10),
		Classes:   s.Int(ctx, KeyPasswordClasses, 3),
	}
}

// ValidatePassword enforces the configured strength.
func (p PasswordPolicy) ValidatePassword(password string) error {
	if len(password) < p.MinLength {
		return fmt.Errorf("password must be at least %d characters", p.MinLength)
	}
	var upper, lower, digit, symbol bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	classes := 0
	for _, present := range []bool{upper, lower, digit, symbol} {
		if present {
			classes++
		}
	}
	if classes < p.Classes {
		return fmt.Errorf("password must mix at least %d character classes", p.Classes)
	}
	return nil
}

// ValidateUsername rejects reserved and malformed names.
func (s *Settings) ValidateUsername(ctx context.Context, username string) error {
	trimmed := strings.TrimSpace(username)
	if len(trimmed) < 3 || len(trimmed) > 32 {
		return fmt.Errorf("username must be 3-32 characters")
	}
	for _, r := range trimmed {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.' {
			return fmt.Errorf("username may only contain letters, digits, dot, dash and underscore")
		}
	}
	lower := strings.ToLower(trimmed)
	for _, reserved := range s.ReservedNames(ctx) {
		if lower == reserved {
			return fmt.Errorf("username %q is reserved", trimmed)
		}
	}
	return nil
}
