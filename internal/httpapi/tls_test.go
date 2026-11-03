package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// newTLSHarness mirrors newHarness but serves over TLS with SecureCookies on,
// which is what every production deployment behind TLS actually runs. Up to
// this round every test used plain HTTP with SecureCookies=false, so the Secure
// cookie path had never been exercised.
func newTLSHarness(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	api, err := httpapi.New(httpapi.Config{
		Store:         store,
		Sessions:      manager,
		Settings:      settings.New(store),
		SecureCookies: true,
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewTLSServer(api.Handler())
	t.Cleanup(server.Close)
	return server
}

func TestSecureCookieOverTLS(t *testing.T) {
	server := newTLSHarness(t)
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar

	// Initialize over TLS and inspect the raw Set-Cookie header: Go's cookiejar
	// deliberately drops attributes when it hands cookies back, so asserting on
	// jar.Cookies() would only test the jar, not what the server sent.
	payload, _ := json.Marshal(map[string]string{
		"username": "root-owner", "password": strongPassword,
	})
	response, err := client.Post(server.URL+"/api/v1/setup", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup over tls = %d", response.StatusCode)
	}
	setCookie := strings.Join(response.Header.Values("Set-Cookie"), "; ")
	if !strings.Contains(setCookie, "et_session=") {
		t.Fatalf("no session cookie in response: %q", setCookie)
	}
	for _, attribute := range []string{"Secure", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(setCookie, attribute) {
			t.Fatalf("session cookie missing %q: %q", attribute, setCookie)
		}
	}

	// The cookie authenticates over TLS.
	status, body := doJSON(t, client, http.MethodGet, server.URL+"/api/v1/console-info", nil)
	if status != http.StatusOK {
		t.Fatalf("console-info over tls = %d %s", status, body)
	}

	// Security headers survive on the TLS listener too.
	health, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer health.Body.Close()
	if health.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing X-Frame-Options on TLS listener")
	}
}

// TestSecureCookieIsNotSentOverPlainHTTP proves the flag actually protects the
// session: a browser will not attach a Secure cookie to an http:// request.
func TestSecureCookieIsNotSentOverPlainHTTP(t *testing.T) {
	server := newTLSHarness(t)
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	if status, body := doJSON(t, client, http.MethodPost, server.URL+"/api/v1/setup", map[string]string{
		"username": "root-owner", "password": strongPassword,
	}); status != http.StatusCreated {
		t.Fatalf("setup = %d %s", status, body)
	}

	plainURL := mustParse(t, "http://"+strings.TrimPrefix(server.URL, "https://"))
	if cookies := jar.Cookies(plainURL); len(cookies) != 0 {
		t.Fatalf("jar offered %d cookie(s) to a plain http origin; Secure cookie leaked", len(cookies))
	}
}
