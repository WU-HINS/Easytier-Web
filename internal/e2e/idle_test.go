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

// TestIdleTunnelIsClosed covers F-031: a peer that opens the socket and then
// goes silent (half open connection, slow loris, dead device) must not keep a
// goroutine and a file descriptor alive forever.
func TestIdleTunnelIsClosed(t *testing.T) {
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
	if _, err := store.CreateUser(ctx, "ivy", hash); err != nil {
		t.Fatalf("user: %v", err)
	}

	policy := session.Policy{Interval: time.Second, Timeout: 6 * time.Second}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, policy)
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
	mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	// One heartbeat so the session is fully established, then silence.
	if _, err := mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "ivy",
		MachineId: mustUUID(t, "bbbb2222-0000-0000-0000-000000000001"),
		Hostname:  "node",
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	select {
	case <-mux.Done():
		// Closed by the server watchdog: good.
	case <-time.After(policy.Timeout + 8*time.Second):
		t.Fatal("idle tunnel was not closed by the watchdog")
	}
}
