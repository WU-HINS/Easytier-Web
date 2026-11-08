package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/httpapi"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type harness struct {
	store   *bal.Store
	manager *session.Manager
	cfg     *settings.Settings
	server  *httptest.Server
	api     *httpapi.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	driver, dsn, cleanup := harnessDB(t)
	t.Cleanup(cleanup)
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: driver, DSN: dsn, AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	cfg := settings.New(store)
	api, err := httpapi.New(httpapi.Config{Store: store, Sessions: manager, Settings: cfg, SecureCookies: false})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return &harness{store: store, manager: manager, cfg: cfg, server: server, api: api}
}

func (h *harness) client(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func (h *harness) do(t *testing.T, client *http.Client, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response.StatusCode, payload
}

func (h *harness) setupAdmin(t *testing.T, client *http.Client, username, password string) {
	t.Helper()
	status, body := h.do(t, client, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": username, "password": password,
	})
	if status != http.StatusCreated {
		t.Fatalf("setup = %d: %s", status, body)
	}
}

func (h *harness) register(t *testing.T, client *http.Client, username, password string) {
	t.Helper()
	status, body := h.do(t, client, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": username, "password": password,
	})
	if status != http.StatusCreated {
		t.Fatalf("register = %d: %s", status, body)
	}
}

const strongPassword = "Sup3r-Secret-Pass!"

// TestSetupGate: nothing is usable before the administrator exists.
func TestSetupGate(t *testing.T) {
	h := newHarness(t)
	client := h.client(t)

	if status, body := h.do(t, client, http.MethodGet, "/api/v1/machines", nil); status != http.StatusConflict ||
		!strings.Contains(string(body), "setup_required") {
		t.Fatalf("pre-setup machines = %d %s, want 409 setup_required", status, body)
	}
	if status, _ := h.do(t, client, http.MethodGet, "/api/v1/setup", nil); status != http.StatusOK {
		t.Fatalf("setup status = %d", status)
	}
	h.setupAdmin(t, client, "root-owner", strongPassword)
	if status, body := h.do(t, client, http.MethodPost, "/api/v1/setup", map[string]string{
		"username": "second-admin", "password": strongPassword,
	}); status != http.StatusConflict || !strings.Contains(string(body), "already_initialized") {
		t.Fatalf("second setup = %d %s", status, body)
	}
	if status, _ := h.do(t, client, http.MethodGet, "/api/v1/machines", nil); status != http.StatusOK {
		t.Fatalf("post-setup machines = %d", status)
	}
}

// TestReservedUsernamesAndPolicy covers the configurable registration rules.
func TestReservedUsernamesAndPolicy(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	// "admin" is reserved by default.
	if status, _ := h.do(t, h.client(t), http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "admin", "password": strongPassword}); status != http.StatusBadRequest {
		t.Fatalf("reserved username status = %d, want 400", status)
	}
	// Weak password.
	if status, _ := h.do(t, h.client(t), http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "weakling", "password": "short"}); status != http.StatusBadRequest {
		t.Fatalf("weak password status = %d, want 400", status)
	}
	// Custom reserved list.
	if status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"reserved_usernames": "root-owner,forbidden",
	}); status != http.StatusOK {
		t.Fatalf("settings = %d %s", status, body)
	}
	if status, _ := h.do(t, h.client(t), http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "forbidden", "password": strongPassword}); status != http.StatusBadRequest {
		t.Fatalf("custom reserved username status = %d, want 400", status)
	}
	// Registration can be closed entirely.
	if status, _ := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"registration_open": false,
	}); status != http.StatusOK {
		t.Fatalf("closing registration failed")
	}
	if status, _ := h.do(t, h.client(t), http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": "anyone", "password": strongPassword}); status != http.StatusForbidden {
		t.Fatalf("closed registration status = %d, want 403", status)
	}
}

// TestPrivilegeMatrix walks the admin surface as a normal user.
func TestPrivilegeMatrix(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	user := h.client(t)
	h.register(t, user, "alice", strongPassword)

	adminEndpoints := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/admin/users", nil},
		{http.MethodPost, "/api/v1/admin/users/1/disabled", map[string]any{"disabled": true}},
		{http.MethodDelete, "/api/v1/admin/users/1", nil},
		{http.MethodGet, "/api/v1/admin/settings", nil},
		{http.MethodPut, "/api/v1/admin/settings", map[string]any{"max_instances_per_user": 99}},
	}
	for _, endpoint := range adminEndpoints {
		status, body := h.do(t, user, endpoint.method, endpoint.path, endpoint.body)
		if status != http.StatusForbidden {
			t.Fatalf("normal user %s %s = %d %s, want 403", endpoint.method, endpoint.path, status, body)
		}
	}

	// Anonymous callers get 401, not 403: no session at all.
	anonymous := h.client(t)
	if status, _ := h.do(t, anonymous, http.MethodGet, "/api/v1/admin/users", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous admin call = %d, want 401", status)
	}
}

// TestAdminCannotMutateTheAdministrator covers the "read only" rule.
func TestAdminCannotMutateTheAdministrator(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	var users struct {
		Users []struct {
			ID       int64  "json:\"id\""
			Username string "json:\"username\""
			Role     string "json:\"role\""
		} "json:\"users\""
	}
	status, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/users", nil)
	if status != http.StatusOK {
		t.Fatalf("list users = %d %s", status, body)
	}
	if err := json.Unmarshal(body, &users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users.Users) != 1 || users.Users[0].Role != bal.RoleAdmin {
		t.Fatalf("users = %+v", users.Users)
	}
	adminID := users.Users[0].ID
	if status, _ := h.do(t, admin, http.MethodDelete, fmt.Sprintf("/api/v1/admin/users/%d", adminID), nil); status != http.StatusForbidden {
		t.Fatalf("delete admin = %d, want 403", status)
	}
	if status, _ := h.do(t, admin, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/disabled", adminID),
		map[string]any{"disabled": true}); status != http.StatusForbidden {
		t.Fatalf("disable admin = %d, want 403", status)
	}

	// The admin surface has no credential or binding writes at all.
	for _, path := range []string{
		fmt.Sprintf("/api/v1/admin/users/%d/password", adminID),
		fmt.Sprintf("/api/v1/admin/users/%d/oidc", adminID),
	} {
		if status, _ := h.do(t, admin, http.MethodPost, path, map[string]any{}); status != http.StatusNotFound &&
			status != http.StatusMethodNotAllowed {
			t.Fatalf("%s = %d, want 404/405 (endpoint must not exist)", path, status)
		}
	}
}

// TestDeleteBlockedByOnlineInstances proves the "cannot delete a user with
// online instances" rule with a real device session.
func TestDeleteBlockedByOnlineInstances(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "bob", strongPassword)

	// Find bob.
	var users struct {
		Users []struct {
			ID       int64  "json:\"id\""
			Username string "json:\"username\""
		} "json:\"users\""
	}
	_, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/users", nil)
	_ = json.Unmarshal(body, &users)
	var bobID int64
	for _, item := range users.Users {
		if item.Username == "bob" {
			bobID = item.ID
		}
	}
	if bobID == 0 {
		t.Fatalf("bob not found in %s", body)
	}

	// No session yet: deletion succeeds.
	if status, payload := h.do(t, admin, http.MethodDelete, fmt.Sprintf("/api/v1/admin/users/%d", bobID), nil); status != http.StatusNoContent {
		t.Fatalf("delete idle user = %d %s", status, payload)
	}

	// Recreate bob and open a device session with one running instance.
	user = h.client(t)
	h.register(t, user, "bob2", strongPassword)
	var users2 struct {
		Users []struct {
			ID       int64  "json:\"id\""
			Username string "json:\"username\""
		} "json:\"users\""
	}
	_, body = h.do(t, admin, http.MethodGet, "/api/v1/admin/users", nil)
	_ = json.Unmarshal(body, &users2)
	var bob2ID int64
	for _, item := range users2.Users {
		if item.Username == "bob2" {
			bob2ID = item.ID
		}
	}
	connectDevice(t, h, "bob2", "aaaa1111-2222-3333-4444-555555555555")

	status, payload := h.do(t, admin, http.MethodDelete, fmt.Sprintf("/api/v1/admin/users/%d", bob2ID), nil)
	if status != http.StatusConflict || !strings.Contains(string(payload), "user_has_online_instances") {
		t.Fatalf("delete busy user = %d %s, want 409 user_has_online_instances", status, payload)
	}
}

// connectDevice opens a control-plane session and reports one running instance.
func connectDevice(t *testing.T, h *harness, username, machineID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		_ = h.manager.Serve(ctx, conn)
	}()

	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	deviceMux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	t.Cleanup(deviceMux.Close)

	_, err = deviceMux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken:               username,
		MachineId:               uuidFromString(t, machineID),
		Hostname:                "node",
		RunningNetworkInstances: []*common.UUID{uuidFromString(t, "99999999-8888-7777-6666-555555555555")},
	})
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Wait until the manager reports the instance.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, snapshot := range h.manager.Snapshots(0) {
			if snapshot.Online && len(snapshot.Instances) > 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("session never became visible")
}

func uuidFromString(t *testing.T, value string) *common.UUID {
	t.Helper()
	var raw [16]byte
	cleaned := strings.ReplaceAll(value, "-", "")
	if len(cleaned) != 32 {
		t.Fatalf("bad uuid %q", value)
	}
	for i := 0; i < 16; i++ {
		var high, low byte
		_, _ = fmt.Sscanf(cleaned[i*2:i*2+1], "%x", &high)
		_, _ = fmt.Sscanf(cleaned[i*2+1:i*2+2], "%x", &low)
		raw[i] = high<<4 | low
	}
	return &common.UUID{
		Part1: uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]),
		Part2: uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7]),
		Part3: uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11]),
		Part4: uint32(raw[12])<<24 | uint32(raw[13])<<16 | uint32(raw[14])<<8 | uint32(raw[15]),
	}
}

var _ = proto.Marshal

// harnessDB picks the database for the API tests. By default they run on a
// throwaway SQLite file; setting EASYTIER_HTTPAPI_PG to a PostgreSQL DSN runs the
// same suite against PostgreSQL in a private schema, so the two dialects cannot
// drift apart unnoticed.
func harnessDB(t *testing.T) (dal.Dialect, string, func()) {
	t.Helper()
	base := os.Getenv("EASYTIER_HTTPAPI_PG")
	if base == "" {
		return dal.SQLite, filepath.Join(t.TempDir(), "et.db"), func() {}
	}
	schema := fmt.Sprintf("harness_%d", time.Now().UnixNano())
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	cleanup := func() {
		_ = admin.Close()
		drop, err := sql.Open("pgx", base)
		if err != nil {
			return
		}
		defer drop.Close()
		_, _ = drop.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return dal.Postgres, base + separator + "options=-c%20search_path%3D" + schema, cleanup
}

// TestHarnessDialectMatchesTheRequest keeps the PostgreSQL run honest: if the
// flag were ignored the whole suite would silently fall back to SQLite and every
// test would still pass.
func TestHarnessDialectMatchesTheRequest(t *testing.T) {
	h := newHarness(t)
	want := dal.SQLite
	if os.Getenv("EASYTIER_HTTPAPI_PG") != "" {
		want = dal.Postgres
	}
	if got := h.store.Dialect(); got != want {
		t.Fatalf("harness dialect = %s, want %s", got, want)
	}
}
