package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
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
)

type replica struct {
	store   *bal.Store
	manager *session.Manager
	server  *httptest.Server
}

// TestMultiReplicaConsistency documents what actually happens when two console
// processes share one database, which is what a naive horizontal scale-out
// (two replicas behind a load balancer) produces.
//
// Sessions are shared through the database, device tunnels are not: they live in
// each process's memory.
func TestMultiReplicaConsistency(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "et.db")

	replicas := make([]*replica, 0, 2)
	for i := 0; i < 2; i++ {
		store, _, err := db.OpenWith(ctx, db.Config{
			Driver: dal.SQLite, DSN: path, AutoAlign: true, Backup: false,
		})
		if err != nil {
			t.Fatalf("open store %d: %v", i, err)
		}
		t.Cleanup(func() { _ = store.Close() })
		manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
		if err != nil {
			t.Fatalf("manager %d: %v", i, err)
		}
		api, err := httpapi.New(httpapi.Config{
			Store: store, Sessions: manager, Settings: settings.New(store), SecureCookies: false,
		})
		if err != nil {
			t.Fatalf("httpapi %d: %v", i, err)
		}
		server := httptest.NewServer(api.Handler())
		t.Cleanup(server.Close)
		replicas = append(replicas, &replica{store: store, manager: manager, server: server})
	}
	first, second := replicas[0], replicas[1]

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// 1. Setup on replica A.
	if status, body := doJSON(t, client, http.MethodPost, first.server.URL+"/api/v1/setup",
		map[string]string{"username": "root-owner", "password": strongPassword}); status != http.StatusCreated {
		t.Fatalf("setup = %d %s", status, body)
	}

	// 2. The session cookie works against replica B: sessions live in the DB.
	if status, body := doJSON(t, client, http.MethodGet, second.server.URL+"/api/v1/console-info", nil); status != http.StatusOK {
		t.Fatalf("session is not shared across replicas (status %d): %s", status, body)
	}

	// 3. A device connects to replica A only.
	me, err := first.store.GetUserByName(ctx, "root-owner")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	connectDeviceTo(t, ctx, first.manager, "root-owner", "55550000-0000-0000-0000-000000000001")

	// 4. Replica A sees it online, replica B sees the device row but offline.
	type machineList struct {
		Machines []struct {
			MachineID string "json:\"machine_id\""
			Online    bool   "json:\"online\""
		} "json:\"machines\""
	}
	readMachines := func(base string) machineList {
		status, body := doJSON(t, client, http.MethodGet, base+"/api/v1/machines", nil)
		if status != http.StatusOK {
			t.Fatalf("machines = %d %s", status, body)
		}
		var parsed machineList
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("decode machines: %v", err)
		}
		return parsed
	}
	onA := readMachines(first.server.URL)
	onB := readMachines(second.server.URL)
	if len(onA.Machines) != 1 || !onA.Machines[0].Online {
		t.Fatalf("replica A machines = %+v, want one online device", onA.Machines)
	}
	if len(onB.Machines) != 1 || onB.Machines[0].Online {
		t.Fatalf("replica B machines = %+v, want the device present but offline", onB.Machines)
	}

	// 5. Device control from replica B fails: the tunnel is not in its memory.
	sessionID := onA.Machines[0].MachineID
	if sessionID == "" {
		sessionID = "55550000-0000-0000-0000-000000000001"
	}
	status, body := doJSON(t, client, http.MethodPost, second.server.URL+"/api/v1/machines/"+sessionID+"/proxy-rpc",
		map[string]any{
			"service_name": "api.instance.PeerManageRpc",
			"method_name":  "list_route",
			"payload":      map[string]any{"instance": map[string]any{"id": map[string]any{}}},
		})
	if status != http.StatusNotFound {
		t.Fatalf("proxy-rpc on replica B = %d %s, want 404 (tunnel lives on replica A)", status, body)
	}

	// 6. The same call on replica A succeeds, proving the difference is
	// topology, not permissions.
	if status, body := doJSON(t, client, http.MethodPost, first.server.URL+"/api/v1/machines/"+sessionID+"/proxy-rpc",
		map[string]any{
			"service_name": "api.instance.PeerManageRpc",
			"method_name":  "list_route",
			"payload":      map[string]any{"instance": map[string]any{"id": map[string]any{}}},
		}); status == http.StatusNotFound {
		t.Fatalf("proxy-rpc on replica A unexpectedly 404: %s", body)
	}
	_ = me
}

func connectDeviceTo(t *testing.T, ctx context.Context, manager *session.Manager, username, machineID string) {
	t.Helper()
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
		_ = manager.Serve(ctx, conn)
	}()
	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	deviceMux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	t.Cleanup(deviceMux.Close)
	if _, err := deviceMux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: username,
		MachineId: uuidFromString(t, machineID),
		Hostname:  "node",
		RunningNetworkInstances: []*common.UUID{
			uuidFromString(t, "66660000-0000-0000-0000-000000000001"),
		},
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(0)) > 0 })
}

var _ = proto.Marshal
