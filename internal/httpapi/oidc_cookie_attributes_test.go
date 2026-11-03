package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/oidc/oidctest"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestOIDCStateCookieKeepsItsAttributes covers F-096: the cookie is set with
// Secure/SameSite/HttpOnly. The clearing branch is a second hand written literal
// in the handler and is still unasserted; driving the callback needs the provider
// round trip, which this test does not do (review team, rounds 173-186).
func TestOIDCStateCookieKeepsItsAttributes(t *testing.T) {
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
		Store:         store,
		Sessions:      manager,
		OIDC:          provider,
		SessionKey:    []byte("test-session-key"),
		SecureCookies: true,
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	initializeConsole(t, server.URL)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(server.URL + "/api/v1/auth/oidc/login")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	setCookie := strings.Join(response.Header.Values("Set-Cookie"), "; ")
	if !strings.Contains(setCookie, "et_oidc_state=") {
		t.Fatalf("the state cookie was not set: %q", setCookie)
	}
	for _, attribute := range []string{"Secure", "SameSite=Lax", "HttpOnly"} {
		if !strings.Contains(setCookie, attribute) {
			t.Fatalf("the state cookie is missing %s: %q", attribute, setCookie)
		}
	}
	if strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("the state cookie was immediately expired while being set: %q", setCookie)
	}
}
