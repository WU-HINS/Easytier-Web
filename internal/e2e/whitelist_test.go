package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestWhitelistedDeviceJoinsByItself covers the quick cluster join half of the
// network settings work: a device listed in the network whitelist becomes a member
// during reconcile without an admin adding it, exactly once, and a device the
// whitelist does not mention stays out.
func TestWhitelistedDeviceJoinsByItself(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "dave", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	wanted := "11111111-1111-1111-1111-111111111111"
	unwanted := "22222222-2222-2222-2222-222222222222"
	for _, machine := range []string{wanted, unwanted} {
		if err := store.UpsertDevice(ctx, bal.Device{
			UserID: user.ID, MachineID: machine, Hostname: machine, FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device %s: %v", machine, err)
		}
	}

	cidr := "10.66.0.0/29"
	allowListed := `{"whitelist":["11111111-1111-1111-1111-111111111111"]}`
	allowNobody := `{"whitelist":["33333333-3333-3333-3333-333333333333"]}`
	for id, advanced := range map[string]string{"net-open": allowListed, "net-closed": allowNobody} {
		value := advanced
		if err := store.CreateNetwork(ctx, bal.Network{
			UserID: user.ID, ID: id, DisplayName: id, NetworkName: id, NetworkSecret: "s",
			NetworkingMethod: "Standalone", VirtualCIDR: &cidr, Advanced: &value,
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("network %s: %v", id, err)
		}
	}

	reconciler := &central.Reconciler{Store: store, Sessions: manager, Interval: time.Second,
		Logf: func(string, ...any) {}}
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if report.Enrolled != 1 {
		t.Fatalf("enrolled = %d, want 1 (only the whitelisted device)", report.Enrolled)
	}
	if _, err := store.GetMemberByDevice(ctx, user.ID, "net-open", wanted); err != nil {
		t.Fatalf("the whitelisted device is not a member: %v", err)
	}
	if _, err := store.GetMemberByDevice(ctx, user.ID, "net-closed", wanted); err == nil {
		t.Fatal("the device joined a network whose whitelist does not mention it")
	}
	if _, err := store.GetMemberByDevice(ctx, user.ID, "net-open", unwanted); err == nil {
		t.Fatal("a device outside the whitelist was added")
	}

	report, err = reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if report.Enrolled != 0 {
		t.Fatalf("second pass enrolled %d more member(s), want 0", report.Enrolled)
	}
	members, err := store.ListMembers(ctx, user.ID, "net-open")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("net-open has %d member(s), want 1", len(members))
	}
}
