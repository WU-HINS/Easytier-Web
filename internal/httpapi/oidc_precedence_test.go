package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestEnvironmentProviderSurvivesAnEmptyDatabase pins the seeding half of the
// precedence rule: EASYTIER_OIDC_* builds a provider at startup, reloadOIDC then
// finds nothing in the settings table and clears the runtime copy, and the
// environment provider has to remain usable.
func TestEnvironmentProviderSurvivesAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	cfg := settings.New(store)
	// The setup gate answers 409 before any OIDC logic runs, so the console has to
	// look initialised for this check.
	if err := cfg.MarkInitialized(ctx); err != nil {
		t.Fatalf("initialise: %v", err)
	}
	seeded := &oidc.Provider{}
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, Settings: cfg, OIDC: seeded,
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	target, err := url.Parse(server.URL + "/api/v1/auth/oidc/config")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	_ = target
	response, err := http.Get(server.URL + "/api/v1/auth/oidc/config")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	defer response.Body.Close()
	var payload struct {
		Enabled bool "json:\"enabled\""
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.Enabled {
		t.Fatal("the environment provider was discarded by an empty settings table")
	}
}
