package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/oidc/oidctest"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestOIDCLoginFlow walks the whole browser flow against a fake provider:
// login redirect -> provider -> callback -> console session.
func TestOIDCLoginFlow(t *testing.T) {
	ctx := context.Background()
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()

	store, _, err := db.OpenWith(ctx, db.Config{
		Driver:    dal.SQLite,
		DSN:       filepath.Join(t.TempDir(), "et.db"),
		AutoAlign: true,
		Backup:    false,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	// The test server speaks http on loopback; allow it explicitly instead of
	// weakening the https rule for real deployments.
	_ = settings.New(store).Store().PutSetting(ctx, "oidc.allow_insecure", "true")

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	provider, err := oidc.New(ctx, oidc.Config{
		Issuer:       idp.Server.URL,
		ClientID:     idp.ClientID,
		ClientSecret: "secret",
		RedirectURL:  "http://console.local/api/v1/auth/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		ButtonLabel:  "Continue with SSO",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	api, err := httpapi.New(httpapi.Config{
		Store:      store,
		Sessions:   manager,
		OIDC:       provider,
		SessionKey: []byte("test-session-key"),
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	initializeConsole(t, server.URL)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. The login page asks whether SSO is available.
	response, err := client.Get(server.URL + "/api/v1/auth/oidc/config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status = %d", response.StatusCode)
	}

	// 2. Starting the flow must redirect to the provider with state + PKCE.
	response, err = client.Get(server.URL + "/api/v1/auth/oidc/login")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if response.StatusCode != http.StatusFound {
		t.Fatalf("login status = %d", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	state := location.Query().Get("state")
	nonce := location.Query().Get("nonce")
	challenge := location.Query().Get("code_challenge")
	if state == "" || nonce == "" || challenge == "" {
		t.Fatalf("authorization redirect is missing parameters: %s", location)
	}

	// The provider will only accept the matching verifier, so it needs the
	// challenge the console generated.
	idp.Expect(nonce, challenge)

	// 3. The provider redirects back to the console.
	callback := server.URL + "/api/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(state)
	response, err = client.Get(callback)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if response.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d", response.StatusCode)
	}
	// The clearing cookie is a second hand written literal in the handler; assert it
	// here because this is the only place that reaches the callback (round 180).
	clearingCookie := ""
	for _, cookie := range response.Header.Values("Set-Cookie") {
		// Only the state cookie is being cleared; the session cookie in the same
		// response also carries SameSite=Lax and would mask a regression here.
		if strings.HasPrefix(cookie, "et_oidc_state=") {
			clearingCookie = cookie
		}
	}
	if !strings.Contains(clearingCookie, "et_oidc_state=") {
		t.Fatalf("the callback did not clear the state cookie: %q", clearingCookie)
	}
	// Secure is config driven (this test runs over plain loopback http); the other
	// two are unconditional and are what the clearing branch used to miss.
	for _, attribute := range []string{"SameSite=Lax", "HttpOnly"} {
		if !strings.Contains(clearingCookie, attribute) {
			t.Fatalf("the clearing cookie is missing %s: %q", attribute, clearingCookie)
		}
	}
	if response.Header.Get("Location") != "/" {
		t.Fatalf("callback redirect = %q", response.Header.Get("Location"))
	}

	// 4. The session cookie now authenticates REST calls.
	response, err = client.Get(server.URL + "/api/v1/console-info")
	if err != nil {
		t.Fatalf("console-info: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("console-info status = %d (session not established)", response.StatusCode)
	}

	// 5. The account was provisioned once, keyed by issuer + subject.
	user, err := store.GetUserByOIDC(ctx, idp.Server.URL, "user-1234")
	if err != nil {
		t.Fatalf("provisioned user: %v", err)
	}
	if user.Username != "alice" {
		t.Fatalf("username = %q, want the preferred_username", user.Username)
	}
	var _ bal.User = user
}

// TestOIDCCallbackRejectsTamperedState proves the state cookie is enforced.
func TestOIDCCallbackRejectsTamperedState(t *testing.T) {
	ctx := context.Background()
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	// The test server speaks http on loopback; allow it explicitly instead of
	// weakening the https rule for real deployments.
	_ = settings.New(store).Store().PutSetting(ctx, "oidc.allow_insecure", "true")
	manager, _ := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	provider, err := oidc.New(ctx, oidc.Config{
		Issuer: idp.Server.URL, ClientID: idp.ClientID, ClientSecret: "secret",
		RedirectURL: "http://console.local/api/v1/auth/oidc/callback",
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	api, _ := httpapi.New(httpapi.Config{Store: store, Sessions: manager, OIDC: provider, SessionKey: []byte("k")})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	initializeConsole(t, server.URL)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if _, err := client.Get(server.URL + "/api/v1/auth/oidc/login"); err != nil {
		t.Fatalf("login: %v", err)
	}
	response, err := client.Get(server.URL + "/api/v1/auth/oidc/callback?code=good-code&state=forged")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("tampered state status = %d, want 400", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "json") {
		t.Fatalf("expected a JSON error body")
	}
}

// initializeConsole runs the first-run setup so OIDC endpoints are reachable.
func initializeConsole(t *testing.T, baseURL string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"username": "root-owner", "password": "Sup3r-Secret-Pass!",
	})
	response, err := http.Post(baseURL+"/api/v1/setup", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("setup = %d: %s", response.StatusCode, body)
	}
}
