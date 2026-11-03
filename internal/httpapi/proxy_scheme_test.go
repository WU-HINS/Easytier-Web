package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

// TestProxySchemeHonoursXForwardedProto covers F-027: behind a TLS terminating
// proxy the request arrives as plain HTTP, so the derived callback used to be
// http and was rejected. The header is only trusted when the operator enabled
// server.trust_proxy, otherwise anyone could claim their request was secure.
func TestProxySchemeHonoursXForwardedProto(t *testing.T) {
	ctx := context.Background()
	idp := oidctest.NewFakeIDP(t)
	defer idp.Server.Close()

	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	cfg := settings.New(store)
	if err := store.PutSetting(ctx, "oidc.allow_insecure", "true"); err != nil {
		t.Fatalf("allow insecure: %v", err)
	}
	if err := cfg.MarkInitialized(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	provider, err := oidc.New(ctx, oidc.Config{
		Issuer: idp.Server.URL, ClientID: idp.ClientID, ClientSecret: "secret",
		Scopes: []string{"openid"},
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, Settings: cfg,
		OIDC: provider, SessionKey: []byte("test-key"),
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	redirectURI := func(header string) string {
		request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/oidc/login", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if header != "" {
			request.Header.Set("X-Forwarded-Proto", header)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want a redirect", response.StatusCode)
		}
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			t.Fatalf("parse location: %v", err)
		}
		return location.Query().Get("redirect_uri")
	}

	// Without trust_proxy the header is ignored: the derived URL stays http.
	if got := redirectURI("https"); !strings.HasPrefix(got, "http://") {
		t.Fatalf("redirect_uri = %q, want http while the proxy is untrusted", got)
	}

	// With trust_proxy enabled the header is honoured.
	if err := store.PutSetting(ctx, settings.KeyTrustProxy, "true"); err != nil {
		t.Fatalf("trust proxy: %v", err)
	}
	if got := redirectURI("https"); !strings.HasPrefix(got, "https://") {
		t.Fatalf("redirect_uri = %q, want https once the proxy is trusted", got)
	}

	// A header claiming something else must not produce https.
	if got := redirectURI("http"); !strings.HasPrefix(got, "http://") {
		t.Fatalf("redirect_uri = %q, want http for X-Forwarded-Proto: http", got)
	}
}
