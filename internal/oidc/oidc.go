// Package oidc implements the OpenID Connect authorization-code flow (with
// PKCE) used by the console login page.
//
// Discovery, JWKS retrieval, JWT signature verification and claim validation
// are implemented here on top of the standard library so the whole flow can be
// exercised against a fake identity provider in tests.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Config is the identity provider configuration.
type Config struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	ButtonLabel   string
	AllowInsecure bool
}

// FromEnv reads the configuration. The boolean reports whether OIDC is enabled.
func FromEnv() (Config, bool, error) {
	issuer := strings.TrimSpace(os.Getenv("EASYTIER_OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("EASYTIER_OIDC_CLIENT_ID"))
	if issuer == "" && clientID == "" {
		return Config{}, false, nil
	}
	if issuer == "" || clientID == "" {
		return Config{}, false, errors.New("oidc: EASYTIER_OIDC_ISSUER and EASYTIER_OIDC_CLIENT_ID must both be set")
	}
	scopes := strings.Fields(os.Getenv("EASYTIER_OIDC_SCOPES"))
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	label := strings.TrimSpace(os.Getenv("EASYTIER_OIDC_BUTTON_LABEL"))
	if label == "" {
		label = "Sign in with SSO"
	}
	allowInsecure := strings.TrimSpace(os.Getenv("EASYTIER_OIDC_ALLOW_INSECURE")) == "1"
	config := Config{
		Issuer:        strings.TrimSuffix(issuer, "/"),
		ClientID:      clientID,
		ClientSecret:  os.Getenv("EASYTIER_OIDC_CLIENT_SECRET"),
		RedirectURL:   strings.TrimSpace(os.Getenv("EASYTIER_OIDC_REDIRECT_URL")),
		Scopes:        scopes,
		ButtonLabel:   label,
		AllowInsecure: allowInsecure,
	}
	if !allowInsecure && !strings.HasPrefix(config.Issuer, "https://") {
		return Config{}, false, errors.New("oidc: issuer must use https (set EASYTIER_OIDC_ALLOW_INSECURE=1 for development)")
	}
	return config, true, nil
}

// Discovery is the subset of provider metadata the flow needs.
type Discovery struct {
	Issuer                string   "json:\"issuer\""
	AuthorizationEndpoint string   "json:\"authorization_endpoint\""
	TokenEndpoint         string   "json:\"token_endpoint\""
	JWKSUri               string   "json:\"jwks_uri\""
	EndSessionEndpoint    string   "json:\"end_session_endpoint\""
	IDTokenSigningAlgs    []string "json:\"id_token_signing_alg_values_supported\""
}

// Claims are the validated identity claims of one login.
type Claims struct {
	Subject           string
	Issuer            string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	Name              string
	Audience          []string
	Expiry            time.Time
}

// cache holds the discovery document and JWKS, shared by every provider view.
type cache struct {
	mu      sync.Mutex
	disco   *Discovery
	discoAt time.Time
	keys    map[string]crypto.PublicKey
	keysAt  time.Time
}

// Provider talks to one identity provider.
type Provider struct {
	cfg    Config
	client *http.Client
	cache  *cache
}

// WithRedirect returns a view of the provider that uses another redirect URL
// while sharing the discovery and JWKS caches. The console needs this because
// the callback URL is produced by the browser, one per request.
func (p *Provider) WithRedirect(redirect string) *Provider {
	if redirect == "" {
		return p
	}
	clone := *p
	clone.cfg.RedirectURL = redirect
	return &clone
}

const (
	discoveryTTL = 10 * time.Minute
	jwksTTL      = 10 * time.Minute
	clockSkew    = 90 * time.Second
)

// New builds a provider and prefetches discovery so a misconfiguration fails at
// startup rather than at first login.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	provider := &Provider{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		cache:  &cache{keys: map[string]crypto.PublicKey{}},
	}
	if _, err := provider.Discovery(ctx); err != nil {
		return nil, err
	}
	return provider, nil
}

// Config exposes the configuration for handlers.
func (p *Provider) Config() Config { return p.cfg }

// Discovery fetches and caches the provider metadata.
func (p *Provider) Discovery(ctx context.Context) (*Discovery, error) {
	p.cache.mu.Lock()
	if p.cache.disco != nil && time.Since(p.cache.discoAt) < discoveryTTL {
		cached := *p.cache.disco
		p.cache.mu.Unlock()
		return &cached, nil
	}
	p.cache.mu.Unlock()

	endpoint := p.cfg.Issuer + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery request: %w", err)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: read discovery: %w", err)
	}
	discovery := &Discovery{}
	if err := json.Unmarshal(body, discovery); err != nil {
		return nil, fmt.Errorf("oidc: decode discovery: %w", err)
	}
	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" || discovery.JWKSUri == "" {
		return nil, errors.New("oidc: discovery document is missing required endpoints")
	}
	if discovery.Issuer != "" && strings.TrimSuffix(discovery.Issuer, "/") != p.cfg.Issuer {
		return nil, fmt.Errorf("oidc: discovery issuer %q does not match configured issuer %q", discovery.Issuer, p.cfg.Issuer)
	}
	p.cache.mu.Lock()
	p.cache.disco = discovery
	p.cache.discoAt = time.Now()
	p.cache.mu.Unlock()
	return discovery, nil
}

// AuthorizationURL builds the redirect target for the login button.
func (p *Provider) AuthorizationURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	discovery, err := p.Discovery(ctx)
	if err != nil {
		return "", err
	}
	target, err := url.Parse(discovery.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("oidc: parse authorization endpoint: %w", err)
	}
	query := target.Query()
	query.Set("response_type", "code")
	query.Set("client_id", p.cfg.ClientID)
	query.Set("redirect_uri", p.cfg.RedirectURL)
	query.Set("scope", strings.Join(p.cfg.Scopes, " "))
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", PKCEChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	target.RawQuery = query.Encode()
	return target.String(), nil
}

// Exchange swaps an authorization code for tokens and validates the ID token.
func (p *Provider) Exchange(ctx context.Context, code, verifier, expectedNonce string) (Claims, string, error) {
	discovery, err := p.Discovery(ctx)
	if err != nil {
		return Claims{}, "", err
	}
	body, err := p.postToken(ctx, discovery.TokenEndpoint, code, verifier, true)
	if err != nil {
		body, err = p.postToken(ctx, discovery.TokenEndpoint, code, verifier, false)
		if err != nil {
			return Claims{}, "", err
		}
	}
	var tokenResponse struct {
		IDToken string "json:\"id_token\""
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return Claims{}, "", fmt.Errorf("oidc: decode token response: %w", err)
	}
	if tokenResponse.IDToken == "" {
		return Claims{}, "", errors.New("oidc: token response has no id_token (is the openid scope requested?)")
	}
	claims, err := p.VerifyIDToken(ctx, tokenResponse.IDToken, expectedNonce)
	if err != nil {
		return Claims{}, "", err
	}
	return claims, tokenResponse.IDToken, nil
}

func (p *Provider) postToken(ctx context.Context, endpoint, code, verifier string, basicAuth bool) ([]byte, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", verifier)
	if !basicAuth && p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oidc: token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if basicAuth && p.cfg.ClientSecret != "" {
		request.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc: token endpoint: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: read token response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: token endpoint returned %s: %s", response.Status, truncate(string(body), 200))
	}
	return body, nil
}

// LogoutURL renders the RP initiated logout URL when the provider supports it.
func (p *Provider) LogoutURL(idTokenHint, postLogoutRedirect string) string {
	discovery, err := p.Discovery(context.Background())
	if err != nil || discovery.EndSessionEndpoint == "" {
		return ""
	}
	target, err := url.Parse(discovery.EndSessionEndpoint)
	if err != nil {
		return ""
	}
	query := target.Query()
	if idTokenHint != "" {
		query.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirect != "" {
		query.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	target.RawQuery = query.Encode()
	return target.String()
}

// PKCEChallenge renders the S256 challenge of a verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// VerifyIDToken validates signature and claims of an ID token.
func (p *Provider) VerifyIDToken(ctx context.Context, token, expectedNonce string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("oidc: id_token is not a compact JWT")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("oidc: decode id_token header: %w", err)
	}
	var header struct {
		Alg string "json:\"alg\""
		Kid string "json:\"kid\""
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, fmt.Errorf("oidc: parse id_token header: %w", err)
	}
	var hash crypto.Hash
	switch header.Alg {
	case "RS256":
		hash = crypto.SHA256
	case "RS384":
		hash = crypto.SHA384
	case "RS512":
		hash = crypto.SHA512
	default:
		return Claims{}, fmt.Errorf("oidc: unsupported id_token alg %q", header.Alg)
	}
	key, err := p.signingKey(ctx, header.Kid)
	if err != nil {
		return Claims{}, err
	}
	signed := []byte(parts[0] + "." + parts[1])
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("oidc: decode id_token signature: %w", err)
	}
	digest := hash.New()
	digest.Write(signed)
	if err := rsa.VerifyPKCS1v15(key, hash, digest.Sum(nil), signature); err != nil {
		return Claims{}, fmt.Errorf("oidc: id_token signature is invalid: %w", err)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("oidc: decode id_token payload: %w", err)
	}
	var payload struct {
		Issuer            string          "json:\"iss\""
		Subject           string          "json:\"sub\""
		Audience          json.RawMessage "json:\"aud\""
		Expiry            int64           "json:\"exp\""
		IssuedAt          int64           "json:\"iat\""
		Nonce             string          "json:\"nonce\""
		Email             string          "json:\"email\""
		EmailVerified     bool            "json:\"email_verified\""
		PreferredUsername string          "json:\"preferred_username\""
		Name              string          "json:\"name\""
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return Claims{}, fmt.Errorf("oidc: parse id_token payload: %w", err)
	}
	audience, err := parseAudience(payload.Audience)
	if err != nil {
		return Claims{}, err
	}
	if strings.TrimSuffix(payload.Issuer, "/") != p.cfg.Issuer {
		return Claims{}, fmt.Errorf("oidc: id_token issuer %q does not match %q", payload.Issuer, p.cfg.Issuer)
	}
	if !containsString(audience, p.cfg.ClientID) {
		return Claims{}, fmt.Errorf("oidc: id_token audience %v does not include %q", audience, p.cfg.ClientID)
	}
	now := time.Now()
	// Expired and not-yet-valid are the two symptoms of a clock that disagrees
	// with the provider, and the bare "has expired" sent operators looking at the
	// provider instead of this host (F-010). Both messages now carry the timestamps
	// and the allowance so the skew is visible in the error itself.
	if payload.Expiry == 0 || now.After(time.Unix(payload.Expiry, 0).Add(clockSkew)) {
		return Claims{}, fmt.Errorf("oidc: id_token has expired (console clock %s, token expired %s, skew allowance %s); check the clock on this host and on the identity provider",
			now.UTC().Format(time.RFC3339), time.Unix(payload.Expiry, 0).UTC().Format(time.RFC3339), clockSkew)
	}
	if payload.IssuedAt != 0 && time.Unix(payload.IssuedAt, 0).After(now.Add(clockSkew)) {
		return Claims{}, fmt.Errorf("oidc: id_token is issued in the future (console clock %s, token issued at %s, skew allowance %s); check the clock on this host and on the identity provider",
			now.UTC().Format(time.RFC3339), time.Unix(payload.IssuedAt, 0).UTC().Format(time.RFC3339), clockSkew)
	}
	if expectedNonce != "" && payload.Nonce != expectedNonce {
		return Claims{}, errors.New("oidc: id_token nonce mismatch")
	}
	if payload.Subject == "" {
		return Claims{}, errors.New("oidc: id_token has no subject")
	}
	return Claims{
		Subject:           payload.Subject,
		Issuer:            payload.Issuer,
		Email:             payload.Email,
		EmailVerified:     payload.EmailVerified,
		PreferredUsername: payload.PreferredUsername,
		Name:              payload.Name,
		Audience:          audience,
		Expiry:            time.Unix(payload.Expiry, 0),
	}, nil
}

func (p *Provider) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if key, ok := p.cachedKey(kid); ok {
		return key, nil
	}
	if err := p.refreshKeys(ctx); err != nil {
		return nil, err
	}
	if key, ok := p.cachedKey(kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("oidc: no signing key for kid %q", kid)
}

func (p *Provider) cachedKey(kid string) (*rsa.PublicKey, bool) {
	p.cache.mu.Lock()
	defer p.cache.mu.Unlock()
	if time.Since(p.cache.keysAt) > jwksTTL {
		return nil, false
	}
	if kid == "" && len(p.cache.keys) == 1 {
		for _, key := range p.cache.keys {
			if rsaKey, ok := key.(*rsa.PublicKey); ok {
				return rsaKey, true
			}
		}
	}
	key, ok := p.cache.keys[kid]
	if !ok {
		return nil, false
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	return rsaKey, ok
}

func (p *Provider) refreshKeys(ctx context.Context) error {
	discovery, err := p.Discovery(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery.JWKSUri, nil)
	if err != nil {
		return fmt.Errorf("oidc: jwks request: %w", err)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("oidc: jwks: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("oidc: read jwks: %w", err)
	}
	var document struct {
		Keys []struct {
			Kty string "json:\"kty\""
			Kid string "json:\"kid\""
			N   string "json:\"n\""
			E   string "json:\"e\""
		} "json:\"keys\""
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return fmt.Errorf("oidc: decode jwks: %w", err)
	}
	keys := map[string]crypto.PublicKey{}
	for _, key := range document.Keys {
		if key.Kty != "RSA" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			continue
		}
		value := 0
		for _, b := range exponent {
			value = value<<8 | int(b)
		}
		if value == 0 {
			continue
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: value}
	}
	if len(keys) == 0 {
		return errors.New("oidc: jwks document has no usable RSA key")
	}
	p.cache.mu.Lock()
	p.cache.keys = keys
	p.cache.keysAt = time.Now()
	p.cache.mu.Unlock()
	return nil
}

func parseAudience(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("oidc: id_token has no audience")
	}
	if raw[0] == '[' {
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("oidc: parse audience: %w", err)
		}
		return list, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("oidc: parse audience: %w", err)
	}
	return []string{single}, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
