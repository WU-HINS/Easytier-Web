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

// TestReconcilePushesCompiledConfig verifies the 2.7.0 "network" behaviour: the
// console compiles the intent and pushes one config per member, and a second
// pass with unchanged intent is a no-op.
func TestReconcilePushesCompiledConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := newTestStore(t, ctx)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := settings.New(store).MarkInitialized(ctx); err != nil {
		t.Fatalf("mark initialized: %v", err)
	}
	hash, err := auth.Hash("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := store.CreateUser(ctx, "bob", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		_ = manager.Serve(ctx, conn)
	}()

	deviceConn, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var (
		mu     sync.Mutex
		pushed []*manage.NetworkConfig
	)
	deviceRegistry := rpc.NewRegistry()
	if err := deviceRegistry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			req := &manage.RunNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, req); err != nil {
				return nil, err
			}
			mu.Lock()
			pushed = append(pushed, req.GetConfig())
			mu.Unlock()
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	deviceMux := rpc.NewMux(deviceConn, deviceRegistry, rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	defer deviceMux.Close()

	machineID := "11111111-2222-3333-4444-555555555555"
	if _, err := deviceMux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "bob",
		MachineId: mustUUID(t, machineID),
		Hostname:  "node-1",
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	cidr := "10.77.0.0/29"
	now := time.Now()
	network := bal.Network{
		UserID: user.ID, ID: "net-reconcile", DisplayName: "reconcile",
		NetworkName: "reconcile-net", NetworkSecret: "s3cret",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}
	if err := store.CreateNetwork(ctx, network); err != nil {
		t.Fatalf("create network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: network.ID, ID: "member-1", DeviceID: machineID,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	reconciler := &central.Reconciler{Store: store, Sessions: manager, Interval: time.Second,
		Logf: func(string, ...any) {}}
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Pushed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}

	mu.Lock()
	if len(pushed) != 1 {
		mu.Unlock()
		t.Fatalf("pushed %d configs, want 1", len(pushed))
	}
	config := pushed[0]
	mu.Unlock()
	if config.GetVirtualIpv4() != "10.77.0.1" {
		t.Fatalf("virtual ip = %s", config.GetVirtualIpv4())
	}
	if config.GetNetworkName() != "reconcile-net" {
		t.Fatalf("network name = %s", config.GetNetworkName())
	}
	if config.GetHostname() != "node-1" {
		t.Fatalf("hostname = %s", config.GetHostname())
	}

	// Unchanged intent must not re-push.
	report, err = reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if report.Pushed != 0 || report.Skipped != 1 {
		t.Fatalf("second report = %+v", report)
	}
	mu.Lock()
	count := len(pushed)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("config pushed %d times, want 1", count)
	}
}
