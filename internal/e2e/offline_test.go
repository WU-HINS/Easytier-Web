package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

type deviceConn struct {
	mux    *rpc.Mux
	cancel context.CancelFunc
}

func (d *deviceConn) close() { d.cancel(); d.mux.Close() }

// connectOnce opens one control-plane connection for a device and sends a
// heartbeat. The caller owns the lifetime so it can simulate a disconnect.
func connectOnce(t *testing.T, manager *session.Manager, username, machineID string, instances []string) *deviceConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = mux.Run(ctx) }()

	heartbeat := &web.HeartbeatRequest{UserToken: username, MachineId: mustUUID(t, machineID), Hostname: "node"}
	for _, instance := range instances {
		heartbeat.RunningNetworkInstances = append(heartbeat.RunningNetworkInstances, mustUUID(t, instance))
	}
	if _, err := mux.Call(ctx, session.WebServerKey, 1, heartbeat); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	return &deviceConn{mux: mux, cancel: cancel}
}

// TestDeviceDisconnectAndReconnect covers the offline path: a dropped tunnel
// must release the session and its instances, and a reconnect must rebuild it.
func TestDeviceDisconnectAndReconnect(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "frank", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	machineID := "77770000-0000-0000-0000-000000000001"
	instanceID := "88880000-0000-0000-0000-000000000001"

	first := connectOnce(t, manager, "frank", machineID, []string{instanceID})
	waitFor(t, 5*time.Second, func() bool { return len(manager.OnlineInstances(user.ID)) == 1 })
	if got := manager.OnlineInstances(user.ID)[machineID]; len(got) != 1 || got[0] != instanceID {
		t.Fatalf("online instances after connect = %v", manager.OnlineInstances(user.ID))
	}

	// Drop the tunnel: the server side must notice and release the session.
	first.close()
	waitFor(t, 10*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 0 })
	if usage := manager.OnlineInstances(user.ID); len(usage) != 0 {
		t.Fatalf("instances still counted after disconnect: %v", usage)
	}
	if live := manager.ConnectedUsers(); len(live) != 0 {
		t.Fatalf("connected users after disconnect = %v", live)
	}

	// Reconnect: the same machine id must register a fresh session.
	second := connectOnce(t, manager, "frank", machineID, []string{instanceID})
	defer second.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })
	if got := manager.OnlineInstances(user.ID)[machineID]; len(got) != 1 {
		t.Fatalf("online instances after reconnect = %v", manager.OnlineInstances(user.ID))
	}

	// The device row must survive the whole cycle (first_seen kept, last_seen
	// refreshed), which is what the device list promises.
	device, err := store.GetDevice(ctx, user.ID, machineID)
	if err != nil {
		t.Fatalf("device after reconnect: %v", err)
	}
	if device.FirstSeen.IsZero() || device.LastSeen.Before(device.FirstSeen) {
		t.Fatalf("device timestamps look wrong: %+v", device)
	}
}
