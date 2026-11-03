package httpapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestBlockDuringHeartbeatStorm covers F-117: blocking a device while its
// heartbeats are in flight must not race with session registration and must end
// with every tunnel of that machine closed.
func TestBlockDuringHeartbeatStorm(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "gina", strongPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	machine := "beef3333-0000-4000-8000-000000000001"

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

	me, err := h.store.GetUserByName(ctx, "gina")
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	const lanes = 4
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for lane := 0; lane < lanes; lane++ {
		tunnel, err := wire.DialTCP(ctx, listener.Addr())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{CallTimeout: 2 * time.Second})
		go func() { _ = mux.Run(ctx) }()
		wg.Add(1)
		go func(m *rpc.Mux) {
			defer wg.Done()
			defer m.Close()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Errors are expected once the session is closed by the block.
				_, _ = m.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
					UserToken: "gina",
					MachineId: uuidFromString(t, machine),
					Hostname:  "node",
				})
				time.Sleep(20 * time.Millisecond)
			}
		}(mux)
	}

	// Let the storm build up, then block the device in the middle of it.
	waitFor(t, 5*time.Second, func() bool { return len(h.manager.Snapshots(me.ID)) > 0 })
	time.Sleep(150 * time.Millisecond)
	if status, body := h.do(t, user, http.MethodDelete, "/api/v1/machines/"+machine+"?block=true", nil); status != http.StatusNoContent {
		close(stop)
		wg.Wait()
		t.Fatalf("delete with block = %d %s", status, body)
	}

	// Every session of that machine must be gone, and nothing may re-register.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.manager.Snapshots(me.ID)) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()

	if snapshots := h.manager.Snapshots(me.ID); len(snapshots) != 0 {
		t.Fatalf("sessions survived the block: %+v", snapshots)
	}
	blocked, err := h.store.IsBlocked(ctx, me.ID, machine)
	if err != nil || !blocked {
		t.Fatalf("device is not blocked: %v %v", blocked, err)
	}
}
