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
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestSetupTokenGatesTheFirstAdmin covers F-136: before initialization anyone who
// could reach the port could claim the admin account. With a token configured the
// winner has to prove they were told it.
func TestSetupTokenGatesTheFirstAdmin(t *testing.T) {
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
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, Settings: settings.New(store),
		SecureCookies: false, SetupToken: "s3cret-setup-token",
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	post := func(token string) int {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/setup",
			strings.NewReader(`{"username":"root-owner","password":"Sup3r-Secret-Pass!"}`))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("X-Setup-Token", token)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}

	if status := post(""); status != http.StatusUnauthorized {
		t.Fatalf("setup without a token = %d, want 401", status)
	}
	if status := post("wrong-token"); status != http.StatusUnauthorized {
		t.Fatalf("setup with a wrong token = %d, want 401", status)
	}
	if status := post("s3cret-setup-token"); status != http.StatusCreated {
		t.Fatalf("setup with the right token = %d, want 201", status)
	}
	if status := post("s3cret-setup-token"); status != http.StatusConflict {
		t.Fatalf("second setup = %d, want 409", status)
	}
	login, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/login",
		strings.NewReader(`{"username":"root-owner","password":"Sup3r-Secret-Pass!"}`))
	login.Header.Set("Content-Type", "application/json")
	response, err := client.Do(login)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login after setup = %d", response.StatusCode)
	}
}
