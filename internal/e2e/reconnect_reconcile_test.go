package e2e_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestReconcileCatchesUpAfterReconnect covers the offline half of the reconcile
// loop: a push to an absent device must fail without recording a revision, and
// the very next pass after the device returns must push and record it exactly
// once.
func TestReconcileCatchesUpAfterReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := newTestStore(t, ctx)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	if err := settings.New(store).MarkInitialized(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	hash, err := auth.Hash("Sup3r-Secret-Pass!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := store.CreateUser(ctx, "gina", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	machineID := "99990000-0000-0000-0000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machineID, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.90.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-offline", DisplayName: "offline", NetworkName: "offline-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-offline", ID: "member-1", DeviceID: machineID,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	reconciler := &central.Reconciler{Store: store, Sessions: manager, Logf: func(string, ...any) {}}

	// 1. Device is offline: the push fails and nothing is recorded.
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile offline: %v", err)
	}
	if report.Pushed != 0 || report.Failed != 1 {
		t.Fatalf("offline report = %+v, want pushed=0 failed=1", report)
	}
	if revision, _ := store.GetManagedConfigRevision(ctx, user.ID, machineID); revision != "" {
		t.Fatalf("revision recorded for a failed push: %q", revision)
	}

	// 2. Device comes back and captures the pushed config.
	var (
		mu       sync.Mutex
		received []*manage.NetworkConfig
	)
	deviceCtx, deviceCancel := context.WithCancel(ctx)
	defer deviceCancel()
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept(deviceCtx)
		if err != nil {
			return
		}
		_ = manager.Serve(deviceCtx, conn)
	}()
	tunnel, err := wire.DialTCP(deviceCtx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			req := &manage.RunNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, req); err != nil {
				return nil, err
			}
			mu.Lock()
			received = append(received, req.GetConfig())
			mu.Unlock()
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	deviceMux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = deviceMux.Run(deviceCtx) }()
	defer deviceMux.Close()
	if _, err := deviceMux.Call(deviceCtx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "gina", MachineId: mustUUID(t, machineID), Hostname: "node",
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	// 3. Next pass pushes and records.
	report, err = reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile online: %v", err)
	}
	if report.Pushed != 1 || report.Failed != 0 {
		t.Fatalf("online report = %+v, want pushed=1 failed=0", report)
	}
	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("device received %d configs, want 1", count)
	}
	revision, _ := store.GetManagedConfigRevision(ctx, user.ID, machineID)
	if revision == "" {
		t.Fatal("revision not recorded after a successful push")
	}

	// 4. Third pass is a no-op: the revision already matches.
	report, err = reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile again: %v", err)
	}
	if report.Pushed != 0 || report.Skipped != 1 {
		t.Fatalf("third report = %+v, want pushed=0 skipped=1", report)
	}
	mu.Lock()
	countAfter := len(received)
	mu.Unlock()
	if countAfter != 1 {
		t.Fatalf("config pushed %d times, want 1", countAfter)
	}
}
