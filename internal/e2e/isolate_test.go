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

// TestBrokenNetworkDoesNotBlockOthers covers F-072: a compile error used to
// abort the whole reconcile pass, so one bad network silently stopped every
// other network of that tenant (and every other tenant).
func TestBrokenNetworkDoesNotBlockOthers(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "sam", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "deadbeef-0000-4000-8000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	goodCIDR := "10.50.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-good", DisplayName: "good", NetworkName: "good-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &goodCIDR,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("good network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-good", ID: "member-good", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("good member: %v", err)
	}
	// A legacy row with an unparsable policy: the API rejects these now, but a
	// database written by an older build can still contain one.
	brokenCIDR := "10.51.0.0/29"
	brokenPolicy := "{not json"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-broken", DisplayName: "broken", NetworkName: "broken-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &brokenCIDR,
		ACLPolicy: &brokenPolicy, CreateTime: now.Add(time.Second), UpdateTime: now,
	}); err != nil {
		t.Fatalf("broken network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-broken", ID: "member-broken", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("broken member: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	conn := connectDeviceForReconcile(t, manager, "sam", machine)
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{Store: store, Sessions: manager, Logf: func(string, ...any) {}}
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile returned an error instead of isolating the broken network: %v", err)
	}
	if report.CompileErrors != 1 {
		t.Fatalf("compile errors = %d, want 1", report.CompileErrors)
	}
	if report.Pushed < 1 {
		t.Fatalf("pushed = %d, want the healthy network to be pushed", report.Pushed)
	}
	revision, err := store.GetManagedConfigRevision(ctx, user.ID, machine)
	if err != nil || revision == "" {
		t.Fatalf("healthy network revision not recorded: %q, %v", revision, err)
	}
}
