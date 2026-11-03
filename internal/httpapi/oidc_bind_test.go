package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/oidc/oidctest"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestOIDCBindRejectsForeignSession covers the account takeover path: a bind
// state signed for one account must not bind the identity to another logged in
// session.
func TestOIDCBindRejectsForeignSession(t *testing.T) {
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
	cfgLayer := settings.New(store)
	if err := cfgLayer.Store().PutSetting(ctx, "oidc.allow_insecure", "true"); err != nil {
		t.Fatalf("allow insecure: %v", err)
	}
	if err := cfgLayer.MarkInitialized(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	hash, err := auth.Hash("Sup3r-Secret-Pass!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	victim, err := store.CreateUser(ctx, "victim", hash)
	if err != nil {
		t.Fatalf("victim: %v", err)
	}
	attacker, err := store.CreateUser(ctx, "attacker", hash)
	if err != nil {
		t.Fatalf("attacker: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	provider, err := oidc.New(ctx, oidc.Config{
		Issuer: idp.Server.URL, ClientID: idp.ClientID, ClientSecret: "secret",
		RedirectURL: "http://console.local/api/v1/auth/oidc/callback", Scopes: []string{"openid"},
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	key := []byte("test-session-key")
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, OIDC: provider, SessionKey: key, SecureCookies: false,
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

	// Log in as the attacker with a password session.
	if status, body := doJSON(t, client, http.MethodPost, server.URL+"/api/v1/auth/login", map[string]string{
		"username": "attacker", "password": "Sup3r-Secret-Pass!",
	}); status != http.StatusOK {
		t.Fatalf("login = %d %s", status, body)
	}

	stateFor := func(userID int64) string {
		bag, err := oidc.EncodeState(key, oidc.StateBag{
			State: "state-1", Nonce: "nonce-1", Verifier: "verifier-1",
			Expires:  time.Now().Add(5 * time.Minute).Unix(),
			Redirect: "http://" + serverAddr(server) + "/api/v1/auth/oidc/callback",
			Mode:     "bind", UserID: userID,
		})
		if err != nil {
			t.Fatalf("encode state: %v", err)
		}
		return bag
	}

	registerChallenge := func(nonce, verifier string) {
		digest := sha256.Sum256([]byte(verifier))
		idp.Expect(nonce, base64.RawURLEncoding.EncodeToString(digest[:]))
	}

	// 1. A state signed for the victim must be refused for the attacker session.
	registerChallenge("nonce-1", "verifier-1")
	request, _ := http.NewRequest(http.MethodGet,
		server.URL+"/api/v1/auth/oidc/callback?code=good-code&state=state-1", nil)
	request.AddCookie(&http.Cookie{Name: "et_oidc_state", Value: stateFor(victim.ID)})
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		body := make([]byte, 256)
		n, _ := response.Body.Read(body)
		t.Fatalf("foreign bind = %d (%s), want 401", response.StatusCode, string(body[:n]))
	} else {
		body := make([]byte, 256)
		n, _ := response.Body.Read(body)
		if !strings.Contains(string(body[:n]), "bind session does not match") {
			t.Fatalf("foreign bind was rejected for the wrong reason: %s", string(body[:n]))
		}
	}
	boundVictim, err := store.GetUserByName(ctx, "victim")
	if err != nil {
		t.Fatalf("victim: %v", err)
	}
	if boundVictim.OIDCSubject != "" {
		t.Fatalf("victim was bound by a foreign session")
	}

	// 2. The same flow with the attacker's own id succeeds.
	registerChallenge("nonce-2", "verifier-2")
	request, _ = http.NewRequest(http.MethodGet,
		server.URL+"/api/v1/auth/oidc/callback?code=good-code&state=state-2", nil)
	own, err := oidc.EncodeState(key, oidc.StateBag{
		State: "state-2", Nonce: "nonce-2", Verifier: "verifier-2",
		Expires:  time.Now().Add(5 * time.Minute).Unix(),
		Redirect: "http://" + serverAddr(server) + "/api/v1/auth/oidc/callback",
		Mode:     "bind", UserID: attacker.ID,
	})
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "et_oidc_state", Value: own})
	response, err = client.Do(request)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		body := make([]byte, 512)
		n, _ := response.Body.Read(body)
		t.Fatalf("own bind = %d (%s), want a redirect", response.StatusCode, string(body[:n]))
	}
	bound, err := store.GetUserByName(ctx, "attacker")
	if err != nil {
		t.Fatalf("attacker: %v", err)
	}
	if bound.OIDCSubject == "" {
		t.Fatalf("the attacker's own bind did not stick")
	}
}

func serverAddr(server *httptest.Server) string {
	return server.URL[len("http://"):]
}
