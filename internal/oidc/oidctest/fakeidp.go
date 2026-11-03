package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// FakeIDP is a minimal OpenID provider used by the tests: discovery, JWKS and
// the token endpoint with PKCE verification and RS256 ID tokens.
type FakeIDP struct {
	Server   *httptest.Server
	Key      *rsa.PrivateKey
	Kid      string
	ClientID string

	mu          sync.Mutex
	nonce       string
	challenge   string
	subject     string
	email       string
	username    string
	expiry      time.Duration
	badSig      bool
	badAudience bool
	alg         string
	issuer      string
	kid         string
}

// NewFakeIDP starts the provider.
func NewFakeIDP(t interface{ Fatalf(string, ...any) }) *FakeIDP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &FakeIDP{
		Key:      key,
		Kid:      "test-key",
		ClientID: "console-client",
		nonce:    "",
		subject:  "user-1234",
		email:    "alice@example.com",
		username: "alice",
		expiry:   time.Hour,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.Server.URL,
			"authorization_endpoint":                idp.Server.URL + "/authorize",
			"token_endpoint":                        idp.Server.URL + "/token",
			"jwks_uri":                              idp.Server.URL + "/jwks",
			"end_session_endpoint":                  idp.Server.URL + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA",
			"kid": idp.Kid,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		verifier := r.Form.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		idp.mu.Lock()
		challenge := idp.challenge
		idp.mu.Unlock()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "good-code" {
			http.Error(w, "invalid grant", http.StatusBadRequest)
			return
		}
		if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
			http.Error(w, "pkce mismatch", http.StatusBadRequest)
			return
		}
		token := idp.signToken()
		writeJSON(w, map[string]any{
			"access_token": "access",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     token,
		})
	})
	idp.Server = httptest.NewServer(mux)
	return idp
}

// Expect records what the next token response should contain.
func (f *FakeIDP) Expect(nonce, challenge string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonce = nonce
	f.challenge = challenge
}

// SetExpiry overrides the ID token lifetime.
func (f *FakeIDP) SetExpiry(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expiry = d
}

// SetBadSignature makes the next ID token carry an invalid signature.
func (f *FakeIDP) SetBadSignature(bad bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.badSig = bad
}

// SetBadAudience makes the next ID token target another client.
func (f *FakeIDP) SetBadAudience(bad bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.badAudience = bad
}

// SetAlg changes the alg header, for testing algorithm confusion defences.
func (f *FakeIDP) SetAlg(alg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alg = alg
}

// SetIssuer overrides the iss claim.
func (f *FakeIDP) SetIssuer(issuer string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issuer = issuer
}

// SetKid overrides the kid header so the console cannot find a key.
func (f *FakeIDP) SetKid(kid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kid = kid
}

func (f *FakeIDP) signToken() string {
	f.mu.Lock()
	nonce := f.nonce
	expiry := f.expiry
	badSignature := f.badSig
	badAudience := f.badAudience
	alg := f.alg
	issuer := f.issuer
	kid := f.kid
	f.mu.Unlock()

	if alg == "" {
		alg = "RS256"
	}
	if issuer == "" {
		issuer = f.Server.URL
	}
	if kid == "" {
		kid = f.Kid
	}

	audience := f.ClientID
	if badAudience {
		audience = "someone-else"
	}
	header := map[string]any{"alg": alg, "kid": kid, "typ": "JWT"}
	payload := map[string]any{
		"iss":                issuer,
		"sub":                f.subject,
		"aud":                audience,
		"exp":                time.Now().Add(expiry).Unix(),
		"iat":                time.Now().Unix(),
		"nonce":              nonce,
		"email":              f.email,
		"email_verified":     true,
		"preferred_username": f.username,
		"name":               "Alice Example",
	}
	if alg == "none" {
		headerJSON, _ := json.Marshal(header)
		payloadJSON, _ := json.Marshal(payload)
		return base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
			base64.RawURLEncoding.EncodeToString(payloadJSON) + "."
	}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)
	signing := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.Key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	if badSignature {
		signature[0] ^= 0xff
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace
