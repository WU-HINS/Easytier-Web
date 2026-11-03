package oidc_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/oidc/oidctest"
)

func newProvider(t *testing.T, idp *oidctest.FakeIDP) *oidc.Provider {
	t.Helper()
	provider, err := oidc.New(context.Background(), oidc.Config{
		Issuer:       idp.Server.URL,
		ClientID:     idp.ClientID,
		ClientSecret: "secret",
		RedirectURL:  "http://console.local/callback",
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func TestAuthorizationURLUsesPKCEAndNonce(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-1234567890-abcdefghijklmnopqrstuvwxyz"
	target, err := provider.AuthorizationURL(context.Background(), "state-1", "nonce-1", verifier)
	if err != nil {
		t.Fatalf("authorization url: %v", err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" {
		t.Fatalf("response_type = %s", query.Get("response_type"))
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("challenge method = %s", query.Get("code_challenge_method"))
	}
	if query.Get("code_challenge") != oidc.PKCEChallenge(verifier) {
		t.Fatalf("challenge mismatch")
	}
	if query.Get("nonce") != "nonce-1" || query.Get("state") != "state-1" {
		t.Fatalf("state/nonce not propagated")
	}
	if !strings.Contains(query.Get("scope"), "openid") {
		t.Fatalf("scope = %s", query.Get("scope"))
	}
}

func TestExchangeValidatesIdToken(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce-ok", oidc.PKCEChallenge(verifier))

	claims, rawToken, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce-ok")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if rawToken == "" {
		t.Fatal("expected raw id_token")
	}
	if claims.Subject != "user-1234" || claims.Email != "alice@example.com" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.PreferredUsername != "alice" {
		t.Fatalf("preferred_username = %s", claims.PreferredUsername)
	}
}

func TestExchangeRejectsWrongNonce(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce-from-provider", oidc.PKCEChallenge(verifier))
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "different-nonce"); err == nil {
		t.Fatal("expected nonce mismatch")
	}
}

func TestExchangeRejectsBadSignature(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce", oidc.PKCEChallenge(verifier))
	idp.SetBadSignature(true)
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
		t.Fatal("expected signature failure")
	}
}

func TestExchangeRejectsWrongAudience(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce", oidc.PKCEChallenge(verifier))
	idp.SetBadAudience(true)
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
		t.Fatal("expected audience failure")
	}
}

func TestExchangeRejectsExpiredToken(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce", oidc.PKCEChallenge(verifier))
	idp.SetExpiry(-time.Hour)
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
		t.Fatal("expected expiry failure")
	}
}

func TestExchangeRejectsWrongPKCEVerifier(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	idp.Expect("nonce", oidc.PKCEChallenge("the-right-verifier-value-1234567890"))
	if _, _, err := provider.Exchange(context.Background(), "good-code", "the-wrong-verifier-value-1234567890", "nonce"); err == nil {
		t.Fatal("expected pkce failure")
	}
}

func TestStateBagRoundTrip(t *testing.T) {
	key := []byte("session-key")
	bag := oidc.StateBag{State: "s", Nonce: "n", Verifier: "v", Expires: time.Now().Add(time.Minute).Unix()}
	encoded, err := oidc.EncodeState(key, bag)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := oidc.DecodeState(key, encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.State != "s" || decoded.Nonce != "n" || decoded.Verifier != "v" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if _, err := oidc.DecodeState([]byte("other-key"), encoded); err == nil {
		t.Fatal("expected signature failure with another key")
	}
	expired := oidc.StateBag{State: "s", Expires: time.Now().Add(-time.Minute).Unix()}
	rawExpired, _ := oidc.EncodeState(key, expired)
	if _, err := oidc.DecodeState(key, rawExpired); err == nil {
		t.Fatal("expected expiry failure")
	}
}

// TestExchangeRejectsAlgorithmConfusion covers the classic JWT bypass: the
// console must only accept the RSA algorithms it knows about, never "none" and
// never an HMAC algorithm keyed with the public key.
func TestExchangeRejectsAlgorithmConfusion(t *testing.T) {
	for _, alg := range []string{"none", "HS256", "ES256", "RS1"} {
		alg := alg
		t.Run(alg, func(t *testing.T) {
			idp := oidctest.NewFakeIDP(t)
			defer idp.Server.Close()
			provider := newProvider(t, idp)

			verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
			idp.Expect("nonce", oidc.PKCEChallenge(verifier))
			idp.SetAlg(alg)
			if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
				t.Fatalf("alg %q was accepted", alg)
			}
		})
	}
}

// TestExchangeRejectsWrongIssuer covers a token minted by a different issuer.
func TestExchangeRejectsWrongIssuer(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce", oidc.PKCEChallenge(verifier))
	idp.SetIssuer("https://evil.example")
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
		t.Fatal("a token from a foreign issuer was accepted")
	}
}

// TestExchangeRejectsUnknownKeyID covers a token signed by a key the console
// does not have in the JWKS.
func TestExchangeRejectsUnknownKeyID(t *testing.T) {
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	provider := newProvider(t, idp)

	verifier := "verifier-abcdefghijklmnopqrstuvwxyz-1234567890"
	idp.Expect("nonce", oidc.PKCEChallenge(verifier))
	idp.SetKid("a-key-that-does-not-exist")
	if _, _, err := provider.Exchange(context.Background(), "good-code", verifier, "nonce"); err == nil {
		t.Fatal("a token with an unknown kid was accepted")
	}
}

// TestStateBagExpires covers the state cookie lifetime: an old bag must not be
// replayable even though its signature is valid.
func TestStateBagExpires(t *testing.T) {
	key := []byte("session-key")
	expired, err := oidc.EncodeState(key, oidc.StateBag{
		State: "s", Nonce: "n", Verifier: "v",
		Expires: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := oidc.DecodeState(key, expired); err == nil {
		t.Fatal("an expired state bag was accepted")
	}

	// A bag without an expiry is treated as invalid, not as "never expires".
	noExpiry, err := oidc.EncodeState(key, oidc.StateBag{State: "s"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := oidc.DecodeState(key, noExpiry); err == nil {
		t.Fatal("a state bag without an expiry was accepted")
	}

	// A bag signed with another key is rejected.
	foreign, err := oidc.EncodeState([]byte("another-key"), oidc.StateBag{
		State: "s", Expires: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := oidc.DecodeState(key, foreign); err == nil {
		t.Fatal("a state bag signed with another key was accepted")
	}
}
