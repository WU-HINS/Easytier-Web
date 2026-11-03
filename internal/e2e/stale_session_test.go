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

// TestSilentDeviceIsExpired covers the case a clean disconnect test cannot: the
// device dies without closing the tunnel (power loss, network partition). The
// console must stop treating it as online after the heartbeat timeout and the
// sweeper must reclaim the session.
func TestSilentDeviceIsExpired(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "henry", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	policy := session.Policy{Interval: time.Second, Timeout: 6 * time.Second}
	if err := policy.Validate(); err != nil {
		t.Fatalf("policy: %v", err)
	}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, policy)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	machineID := "aaaa1111-0000-0000-0000-000000000002"
	device := connectOnce(t, manager, "henry", machineID, []string{"aaaa1111-0000-0000-0000-000000000003"})
	defer device.close()

	snapshots := manager.Snapshots(user.ID)
	if len(snapshots) != 1 || !snapshots[0].Online {
		t.Fatalf("fresh session = %+v, want one online session", snapshots)
	}

	// Stay silent past the timeout while the tunnel remains open.
	time.Sleep(7 * time.Second)

	// The idle watchdog closes a silent tunnel at the heartbeat timeout, so
	// the session is either marked offline or already reclaimed. Both are
	// correct; what must never happen is a silent peer still counted as online.
	snapshots = manager.Snapshots(user.ID)
	for _, snapshot := range snapshots {
		if snapshot.Online {
			t.Fatalf("silent device still reported online after the timeout")
		}
	}
	if usage := manager.OnlineInstances(user.ID); len(usage) != 0 {
		t.Fatalf("silent device still contributes instances: %v", usage)
	}

	// Whatever the watchdog left behind is reclaimed by the sweeper.
	manager.SweepStale()
	if len(manager.Snapshots(user.ID)) != 0 {
		t.Fatalf("session survived the sweeper")
	}
}

var _ = web.HeartbeatRequest{}
var _ = rpc.NewRegistry
var _ = wire.ListenTCP
