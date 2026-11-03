package oidc_test

import (
	"testing"

	"github.com/easytier/easytier-console/internal/oidc"
)

func TestFromEnvRequiresBothIssuerAndClient(t *testing.T) {
	t.Setenv("EASYTIER_OIDC_ISSUER", "")
	t.Setenv("EASYTIER_OIDC_CLIENT_ID", "")
	if _, enabled, err := oidc.FromEnv(); err != nil || enabled {
		t.Fatalf("disabled config = %v, %v", enabled, err)
	}
	t.Setenv("EASYTIER_OIDC_ISSUER", "https://id.example.com")
	if _, _, err := oidc.FromEnv(); err == nil {
		t.Fatal("expected error for missing client id")
	}
}

func TestFromEnvRejectsPlaintextIssuerUnlessAllowed(t *testing.T) {
	t.Setenv("EASYTIER_OIDC_ISSUER", "http://id.example.com/")
	t.Setenv("EASYTIER_OIDC_CLIENT_ID", "client")
	t.Setenv("EASYTIER_OIDC_ALLOW_INSECURE", "")
	if _, _, err := oidc.FromEnv(); err == nil {
		t.Fatal("expected https requirement")
	}
	t.Setenv("EASYTIER_OIDC_ALLOW_INSECURE", "1")
	config, enabled, err := oidc.FromEnv()
	if err != nil || !enabled {
		t.Fatalf("insecure dev config = %v, %v", enabled, err)
	}
	if config.Issuer != "http://id.example.com" {
		t.Fatalf("issuer = %s (trailing slash must be trimmed)", config.Issuer)
	}
	if config.ButtonLabel != "Sign in with SSO" {
		t.Fatalf("label = %s", config.ButtonLabel)
	}
}
