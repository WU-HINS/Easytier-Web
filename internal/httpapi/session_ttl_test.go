package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestSessionTTLReachesBothTheCookieAndTheRow covers F-146: the session lifetime
// existed only as a constant. The cookie Expires and the stored row expiry are
// written in two places, so a future edit could easily change one and not the
// other, leaving a browser that keeps a dead cookie or loses a live one.
func TestSessionTTLReachesBothTheCookieAndTheRow(t *testing.T) {
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	cfg := settings.New(store)
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	const ttl = 90 * time.Minute
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, Settings: cfg,
		SecureCookies: false, SessionTTL: ttl,
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	// Create the single administrator, which also issues a session.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	body := strings.NewReader("{\"username\":\"root-owner\",\"password\":\"Sup3r-Secret-Pass!\"}")
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/setup", body)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup = %d", response.StatusCode)
	}

	cookies := response.Cookies()
	if len(cookies) == 0 {
		t.Fatal("setup did not set a session cookie")
	}
	sessionCookie := cookies[0]
	if !sessionCookie.HttpOnly {
		t.Fatal("the session cookie is not HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", sessionCookie.SameSite)
	}
	wanted := time.Now().Add(ttl)
	if diff := sessionCookie.Expires.Sub(wanted); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("cookie Expires = %v, want about %v", sessionCookie.Expires, wanted)
	}

	// The stored row must agree with the cookie.
	stored, err := store.GetSession(ctx, sessionCookie.Value)
	if err != nil {
		t.Fatalf("stored session: %v", err)
	}
	if diff := stored.Expiry.Sub(wanted); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("stored expiry = %v, want about %v", stored.Expiry, wanted)
	}
	// And the row must not outlive the cookie by much either.
	if stored.Expiry.After(sessionCookie.Expires.Add(time.Minute)) {
		t.Fatalf("the row outlives the cookie: row %v cookie %v", stored.Expiry, sessionCookie.Expires)
	}
}
