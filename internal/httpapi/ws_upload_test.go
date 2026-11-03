package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
)

// wsHarness serves the console with a short heartbeat policy so idle tests do
// not dominate the suite.
type wsHarness struct {
	server  *httptest.Server
	manager *session.Manager
	store   *bal.Store
}

func newWSHarness(t *testing.T) *wsHarness {
	return newWSHarnessWithLimit(t, 0)
}

func newWSHarnessWithLimit(t *testing.T, maxUploads int) *wsHarness {
	t.Helper()
	ctx := context.Background()
	store, _, err := db.OpenWith(ctx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := settings.New(store)
	if err := cfg.MarkInitialized(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	policy := session.Policy{Interval: time.Second, Timeout: 6 * time.Second}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, policy)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	api, err := httpapi.New(httpapi.Config{
		Store: store, Sessions: manager, Settings: cfg, SecureCookies: false,
		MaxUploadConns: maxUploads,
	})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return &wsHarness{server: server, manager: manager, store: store}
}

func (h *wsHarness) uploadURL(username string) string {
	return "ws" + strings.TrimPrefix(h.server.URL, "http") + "/@up/" + username
}

// TestWSUploadChannelCarriesHeartbeat is the end to end check for the feature
// "instances listen on ws on the same port as the panel at /@up/<user>".
// Before this test the whole WebSocket upload path had never been exercised:
// every other session test dials the TCP control plane.
func TestWSUploadChannelCarriesHeartbeat(t *testing.T) {
	h := newWSHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, err := h.store.CreateUser(ctx, "wsuser", "hash")
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.uploadURL("wsuser"), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	tunnel := &wsClientTunnel{conn: conn}
	mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	if _, err := mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken:               "wsuser",
		MachineId:               uuidFromString(t, "cccc3333-0000-0000-0000-000000000001"),
		Hostname:                "ws-node",
		RunningNetworkInstances: []*common.UUID{uuidFromString(t, "dddd4444-0000-0000-0000-000000000001")},
	}); err != nil {
		t.Fatalf("heartbeat over ws: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool { return len(h.manager.Snapshots(user.ID)) == 1 })
	snapshot := h.manager.Snapshots(user.ID)[0]
	if !snapshot.Online || snapshot.Hostname != "ws-node" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// The device row is written by the heartbeat, so the WS path must persist too.
	if _, err := h.store.GetDevice(ctx, user.ID, "cccc3333-0000-0000-0000-000000000001"); err != nil {
		t.Fatalf("device not persisted over ws: %v", err)
	}
}

// TestWSUploadRejectsBadPathIdentity ensures the username in the path is checked
// against the heartbeat token.
func TestWSUploadRejectsBadPathIdentity(t *testing.T) {
	h := newWSHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := h.store.CreateUser(ctx, "alice", "hash"); err != nil {
		t.Fatalf("user: %v", err)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.uploadURL("alice"), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()
	mux := rpc.NewMux(&wsClientTunnel{conn: conn}, rpc.NewRegistry(), rpc.Options{CallTimeout: 2 * time.Second})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	// Token says bob, path says alice: the session must be refused.
	_, err = mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "bob",
		MachineId: uuidFromString(t, "eeee5555-0000-0000-0000-000000000001"),
		Hostname:  "node",
	})
	if err == nil {
		t.Fatal("heartbeat with a mismatched upload path was accepted")
	}
}

// TestWSUploadRejectsOversizedMessage proves the WebSocket read limit is
// enforced and the HTTP listener survives.
func TestWSUploadRejectsOversizedMessage(t *testing.T) {
	h := newWSHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.uploadURL("whoever"), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	// The server may reset the connection while the oversized frame is still
	// being written, so a write error is a valid rejection signal; only a
	// successful write *and* a successful read would mean it was accepted.
	writeErr := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 2<<20))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, readErr := conn.ReadMessage()
	if writeErr == nil && readErr == nil {
		t.Fatal("server accepted an oversized frame")
	}

	// The listener must still serve normal traffic.
	response, err := http.Get(h.server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz after oversized frame: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", response.StatusCode)
	}
}

// wsClientTunnel adapts a gorilla client connection to wire.Tunnel.
type wsClientTunnel struct {
	conn *websocket.Conn
}

func (t *wsClientTunnel) Send(ctx context.Context, p *wire.Packet) error {
	return t.conn.WriteMessage(websocket.BinaryMessage, p.Encode())
}

func (t *wsClientTunnel) Recv(ctx context.Context) (*wire.Packet, error) {
	_, data, err := t.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	return wire.DecodePacket(data)
}

func (t *wsClientTunnel) RemoteAddr() string { return t.conn.RemoteAddr().String() }
func (t *wsClientTunnel) LocalAddr() string  { return t.conn.LocalAddr().String() }
func (t *wsClientTunnel) Close() error       { return t.conn.Close() }

var _ = proto.Marshal

// TestWSUploadPathGivesDiagnosableError covers F-032: a manager pointed at
// /@up or /@up/ without a username used to receive the single page app with
// HTTP 200, which reads as "the console is fine" in device logs.
func TestWSUploadPathGivesDiagnosableError(t *testing.T) {
	h := newWSHarness(t)
	for _, path := range []string{"/@up", "/@up/"} {
		response, err := http.Get(h.server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s = %d, want 400", path, response.StatusCode)
		}
		if !strings.Contains(response.Header.Get("Content-Type"), "json") {
			t.Fatalf("GET %s returned %q, want a JSON error", path, response.Header.Get("Content-Type"))
		}
	}
}

// TestWSUploadConcurrencyIsBounded covers F-033: concurrent device channels are
// capped so a flood of half open sockets cannot pin unbounded goroutines.
func TestWSUploadConcurrencyIsBounded(t *testing.T) {
	h := newWSHarnessWithLimit(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	opened := make([]*websocket.Conn, 0, 2)
	for i := 0; i < 2; i++ {
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, h.uploadURL("slotter"), nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		opened = append(opened, conn)
	}
	defer func() {
		for _, conn := range opened {
			_ = conn.Close()
		}
	}()

	_, response, err := websocket.DefaultDialer.DialContext(ctx, h.uploadURL("slotter"), nil)
	if err == nil {
		t.Fatal("third upload channel was accepted beyond the configured bound")
	}
	if response == nil || response.StatusCode != http.StatusServiceUnavailable {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("third dial status = %d, want 503", status)
	}
}
