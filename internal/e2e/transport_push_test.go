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

// deviceOnTransport dials the console over one transport and counts the configs
// that arrive on it.
type deviceOnTransport struct {
	name    string
	mux     *rpc.Mux
	mu      sync.Mutex
	configs int
}

func (d *deviceOnTransport) pushes() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.configs
}

func dialDevice(t *testing.T, ctx context.Context, name string, conn wire.Tunnel) *deviceOnTransport {
	t.Helper()
	device := &deviceOnTransport{name: name}
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			req := &manage.RunNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, req); err != nil {
				return nil, err
			}
			device.mu.Lock()
			device.configs++
			device.mu.Unlock()
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	device.mux = rpc.NewMux(conn, registry, rpc.Options{})
	go func() { _ = device.mux.Run(ctx) }()
	t.Cleanup(func() { device.mux.Close() })
	return device
}

// TestOneDeviceOnThreeTransportsGetsOnePush is the end to end half of the rule
// decided for this goal: ws, tcp and udp may all be connected at once, and the
// reconciler still configures the device exactly once, on tcp.
func TestOneDeviceOnThreeTransportsGetsOnePush(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "carol", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	tcpListener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	defer tcpListener.Close()
	wsListener, err := wire.ListenWS("127.0.0.1:0", func() bool { return false })
	if err != nil {
		t.Fatalf("ws listen: %v", err)
	}
	defer wsListener.Close()
	udpListener, err := wire.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	defer udpListener.Close()

	for _, listener := range []wire.Listener{tcpListener, wsListener, udpListener} {
		go func(l wire.Listener) {
			conn, err := l.Accept(ctx)
			if err != nil {
				return
			}
			_ = manager.Serve(ctx, conn)
		}(listener)
	}

	tcpConn, err := wire.DialTCP(ctx, tcpListener.Addr())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	wsConn, err := wire.DialWS(ctx, "ws://"+wsListener.Addr()+"/", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	udpConn, err := wire.DialUDP(ctx, udpListener.Addr())
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}

	machineID := "99999999-8888-7777-6666-555555555555"
	devices := []*deviceOnTransport{
		dialDevice(t, ctx, "tcp", tcpConn),
		dialDevice(t, ctx, "ws", wsConn),
		dialDevice(t, ctx, "udp", udpConn),
	}
	for _, device := range devices {
		if _, err := device.mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
			UserToken: "carol",
			MachineId: mustUUID(t, machineID),
			Hostname:  device.name,
		}); err != nil {
			t.Fatalf("heartbeat over %s: %v", device.name, err)
		}
	}

	cidr := "10.88.0.0/29"
	now := time.Now()
	network := bal.Network{
		UserID: user.ID, ID: "net-transport", DisplayName: "transport",
		NetworkName: "transport-net", NetworkSecret: "s3cret",
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
	if _, err := reconciler.ReconcileUser(ctx, user.ID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	total := 0
	for _, device := range devices {
		total += device.pushes()
	}
	if total != 1 {
		t.Fatalf("the device was configured %d time(s) across three transports, want 1", total)
	}
	if got := devices[0].pushes(); got != 1 {
		t.Fatalf("tcp received %d push(es), want 1 (it is the preferred transport)", got)
	}
	for _, device := range devices[1:] {
		if device.pushes() != 0 {
			t.Fatalf("%s received a push although tcp was online", device.name)
		}
	}
}
