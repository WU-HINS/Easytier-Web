package e2e_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestQuotaEnforcedOnHeartbeatPath covers F-004: a device that starts instances
// on its own must be trimmed back to the allowance by the console, not merely
// counted.
func TestQuotaEnforcedOnHeartbeatPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	user, err := store.CreateUser(ctx, "dave", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
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

	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var (
		mu       sync.Mutex
		retained []*manage.RetainNetworkInstanceRequest
	)
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRetainNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			req := &manage.RetainNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, req); err != nil {
				return nil, err
			}
			mu.Lock()
			retained = append(retained, req)
			mu.Unlock()
			return proto.Marshal(&manage.RetainNetworkInstanceResponse{})
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	deviceMux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	defer deviceMux.Close()

	machineID := "33330000-0000-0000-0000-000000000001"
	reported := []string{
		"44440000-0000-0000-0000-000000000003",
		"44440000-0000-0000-0000-000000000001",
		"44440000-0000-0000-0000-000000000002",
	}
	heartbeat := &web.HeartbeatRequest{
		UserToken: "dave",
		MachineId: mustUUID(t, machineID),
		Hostname:  "node",
	}
	for _, instance := range reported {
		heartbeat.RunningNetworkInstances = append(heartbeat.RunningNetworkInstances, mustUUID(t, instance))
	}
	if _, err := deviceMux.Call(ctx, session.WebServerKey, 1, heartbeat); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{Store: store, Sessions: manager, Logf: func(string, ...any) {}}
	report, err := reconciler.EnforceQuota(ctx, user.ID, manager, 2)
	if err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if report.Trimmed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want trimmed=1 failed=0", report)
	}

	waitFor(t, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(retained) == 1
	})
	mu.Lock()
	request := retained[0]
	mu.Unlock()
	if len(request.InstIds) != 2 {
		t.Fatalf("retained %d instances, want 2", len(request.InstIds))
	}
	got := []string{
		session.UUIDString(request.InstIds[0]),
		session.UUIDString(request.InstIds[1]),
	}
	want := []string{
		"44440000-0000-0000-0000-000000000001",
		"44440000-0000-0000-0000-000000000002",
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("retained = %v, want the two smallest ids %v", got, want)
		}
	}

	// A tenant within budget is left alone.
	report, err = reconciler.EnforceQuota(ctx, user.ID, manager, 5)
	if err != nil {
		t.Fatalf("enforce under quota: %v", err)
	}
	if report.Trimmed != 0 || report.Machines != 0 {
		t.Fatalf("report under quota = %+v, want no action", report)
	}

	// A quota of zero means unlimited.
	report, err = reconciler.EnforceQuota(ctx, user.ID, manager, 0)
	if err != nil || report.Trimmed != 0 {
		t.Fatalf("quota 0 = %+v, %v", report, err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

var _ = common.UUID{}
