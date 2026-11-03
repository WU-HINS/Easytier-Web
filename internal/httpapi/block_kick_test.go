package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestBlockingDisconnectsLiveDevice covers F-112: blocking only rejected the next
// heartbeat, so an already connected device kept its tunnel - in practice until
// it happened to reconnect.
func TestBlockingDisconnectsLiveDevice(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "dora2", strongPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	machine := "beef2222-0000-4000-8000-000000000001"

	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept(ctx)
			if err != nil {
				return
			}
			go func() { _ = h.manager.Serve(ctx, conn) }()
		}
	}()
	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()
	if _, err := mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "dora2",
		MachineId: uuidFromString(t, machine),
		Hostname:  "node",
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	me, err := h.store.GetUserByName(ctx, "dora2")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return len(h.manager.Snapshots(me.ID)) == 1 })

	// Block through the API, exactly as the UI does.
	if status, body := h.do(t, user, http.MethodDelete, "/api/v1/machines/"+machine+"?block=true", nil); status != http.StatusNoContent {
		t.Fatalf("delete with block = %d %s", status, body)
	}

	// The live session must be gone right away.
	waitFor(t, 5*time.Second, func() bool { return len(h.manager.Snapshots(me.ID)) == 0 })

	// And a fresh connection is refused.
	fresh, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial again: %v", err)
	}
	freshMux := rpc.NewMux(fresh, rpc.NewRegistry(), rpc.Options{CallTimeout: 2 * time.Second})
	go func() { _ = freshMux.Run(ctx) }()
	defer freshMux.Close()
	if _, err := freshMux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "dora2",
		MachineId: uuidFromString(t, machine),
		Hostname:  "node",
	}); err == nil {
		t.Fatal("a blocked device was allowed to connect again")
	}
	_ = proto.Marshal
}
