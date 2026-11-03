package e2e_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"

	"google.golang.org/protobuf/proto"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestManyDevicesConcurrently pushes the session manager, the reconciler and the
// quota enforcement at the same time. Run it with -race: the point is that
// concurrent readers (snapshots, quota scan) and writers (register, forget,
// heartbeat) must not race or deadlock.
func TestManyDevicesConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	user, err := store.CreateUser(ctx, "stress", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.80.0.0/20"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-stress", DisplayName: "stress", NetworkName: "stress-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Logf: func(string, ...any) {},
		MaxInstancesPerUser: func(context.Context, int64) int { return 0 },
	}

	const devices = 16
	connections := make([]*deviceConn, 0, devices)
	var connMu sync.Mutex

	// Register the devices as members first so the reconciler has work to do.
	for i := 0; i < devices; i++ {
		machine := fmt.Sprintf("f0000000-0000-0000-0000-%012d", i+1)
		if err := store.UpsertDevice(ctx, bal.Device{
			UserID: user.ID, MachineID: machine, Hostname: fmt.Sprintf("node-%02d", i),
			FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
		if err := store.AddMember(ctx, bal.NetworkMember{
			UserID: user.ID, NetworkID: "net-stress", ID: fmt.Sprintf("member-%02d", i),
			DeviceID: machine, CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("member %d: %v", i, err)
		}
	}

	// Connect them all at once.
	var connectWG sync.WaitGroup
	for i := 0; i < devices; i++ {
		connectWG.Add(1)
		go func(index int) {
			defer connectWG.Done()
			conn := connectOnce(t, manager, "stress", fmt.Sprintf("f0000000-0000-0000-0000-%012d", index+1),
				[]string{fmt.Sprintf("e0000000-0000-0000-0000-%012d", index+1)})
			connMu.Lock()
			connections = append(connections, conn)
			connMu.Unlock()
		}(i)
	}

	// While they connect, hammer the read paths and the reconcile loop.
	var workWG sync.WaitGroup
	stopWork := make(chan struct{})
	for i := 0; i < 4; i++ {
		workWG.Add(1)
		go func() {
			defer workWG.Done()
			for {
				select {
				case <-stopWork:
					return
				default:
					_, _ = reconciler.ReconcileUser(ctx, user.ID)
					_ = manager.Snapshots(user.ID)
					_ = manager.OnlineInstances(user.ID)
					_ = manager.ConnectedUsers()
				}
			}
		}()
	}

	connectWG.Wait()
	close(stopWork)
	workWG.Wait()

	waitFor(t, 10*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == devices })
	if got := len(manager.OnlineInstances(user.ID)); got != devices {
		t.Fatalf("online machines = %d, want %d", got, devices)
	}

	// Drop them all at once; the manager must converge back to zero.
	var dropWG sync.WaitGroup
	connMu.Lock()
	snapshot := append([]*deviceConn(nil), connections...)
	connMu.Unlock()
	for _, conn := range snapshot {
		dropWG.Add(1)
		go func(c *deviceConn) {
			defer dropWG.Done()
			c.close()
		}(conn)
	}
	dropWG.Wait()
	waitFor(t, 15*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 0 })
}

// TestConcurrentReconcileConverges runs several reconcile passes over the same
// device at once. Pushes may duplicate (that is idempotent), but the recorded
// revision must end up exactly once and equal to the compiled plan.
func TestConcurrentReconcileConverges(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "racer", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "abcd0000-0000-0000-0000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.70.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-race", DisplayName: "race", NetworkName: "race-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-race", ID: "member-race", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	conn := connectDeviceForReconcile(t, manager, "racer", machine)
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Logf: func(string, ...any) {},
	}
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		pushed int
		failed int
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := reconciler.ReconcileUser(ctx, user.ID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return
			}
			pushed += report.Pushed
		}()
	}
	wg.Wait()

	if failed != 0 {
		t.Fatalf("%d concurrent reconcile calls returned an error", failed)
	}
	if pushed < 1 {
		t.Fatalf("no push happened at all")
	}
	revision, err := store.GetManagedConfigRevision(ctx, user.ID, machine)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if revision == "" {
		t.Fatal("revision not recorded after concurrent pushes")
	}
	// A follow-up pass must be a no-op: the revision already matches.
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("follow-up reconcile: %v", err)
	}
	if report.Pushed != 0 || report.Skipped != 1 {
		t.Fatalf("follow-up report = %+v, want pushed=0 skipped=1", report)
	}
}

// connectDeviceForReconcile opens a device channel that answers
// RunNetworkInstance, which is what the reconciler calls.
func connectDeviceForReconcile(t *testing.T, manager *session.Manager, username, machineID string) *deviceConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		cancel()
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
		cancel()
		t.Fatalf("dial: %v", err)
	}
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		cancel()
		t.Fatalf("register: %v", err)
	}
	mux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	if _, err := mux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: username,
		MachineId: mustUUID(t, machineID),
		Hostname:  "node",
	}); err != nil {
		cancel()
		t.Fatalf("heartbeat: %v", err)
	}
	return &deviceConn{mux: mux, cancel: cancel}
}

// TestReconcileLoopPushesWithoutManualTrigger covers the loop itself: with Run
// started and a device online, the config must arrive without anyone calling
// ReconcileUser. This is the wiring that was missing from the daemon (F-041).
func TestReconcileLoopPushesWithoutManualTrigger(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "loopy", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "beef0000-0000-0000-0000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.60.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-loop", DisplayName: "loop", NetworkName: "loop-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-loop", ID: "member-loop", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	conn := connectDeviceForReconcile(t, manager, "loopy", machine)
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Interval: 100 * time.Millisecond,
		Logf: func(string, ...any) {},
	}
	go reconciler.Run(ctx)

	// Nobody triggers a reconcile here: the loop has to do it on its own.
	waitFor(t, 10*time.Second, func() bool {
		revision, err := store.GetManagedConfigRevision(ctx, user.ID, machine)
		return err == nil && revision != ""
	})
}

// TestTwoNetworksPerDeviceConverge covers F-085: the revision row is keyed by
// device, so a single-value comparison made a device in two networks re-push
// both configs on every pass, forever.
func TestTwoNetworksPerDeviceConverge(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "walt", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "ffff0000-0000-4000-8000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	for index, networkID := range []string{"net-one", "net-two"} {
		cidr := "10.80." + string(rune('0'+index)) + ".0/29"
		if err := store.CreateNetwork(ctx, bal.Network{
			UserID: user.ID, ID: networkID, DisplayName: networkID, NetworkName: networkID,
			NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
			CreateTime: now.Add(time.Duration(index) * time.Second), UpdateTime: now,
		}); err != nil {
			t.Fatalf("network %s: %v", networkID, err)
		}
		if err := store.AddMember(ctx, bal.NetworkMember{
			UserID: user.ID, NetworkID: networkID, ID: "member-" + networkID, DeviceID: machine,
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("member %s: %v", networkID, err)
		}
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	conn := connectDeviceForReconcile(t, manager, "walt", machine)
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{Store: store, Sessions: manager, Logf: func(string, ...any) {}}

	first, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if first.Pushed != 2 {
		t.Fatalf("first pass pushed = %d, want 2", first.Pushed)
	}
	second, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if second.Pushed != 0 || second.Skipped != 2 {
		t.Fatalf("second pass = pushed %d skipped %d, want 0/2 (configs must converge)", second.Pushed, second.Skipped)
	}
	third, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("third pass: %v", err)
	}
	if third.Pushed != 0 {
		t.Fatalf("third pass pushed = %d, want 0", third.Pushed)
	}
}

// TestOrphanInstanceIsStopped covers F-086: removing a membership used to leave
// the instance running on the device, so the removed peer kept network access.
func TestOrphanInstanceIsStopped(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "xena", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "abcd1111-0000-4000-8000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	keepCIDR, dropCIDR := "10.90.0.0/29", "10.91.0.0/29"
	for _, network := range []struct {
		id   string
		cidr string
	}{{"net-keep", keepCIDR}, {"net-drop", dropCIDR}} {
		if err := store.CreateNetwork(ctx, bal.Network{
			UserID: user.ID, ID: network.id, DisplayName: network.id, NetworkName: network.id,
			NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &network.cidr,
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("network %s: %v", network.id, err)
		}
		if err := store.AddMember(ctx, bal.NetworkMember{
			UserID: user.ID, NetworkID: network.id, ID: "member-" + network.id, DeviceID: machine,
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("member %s: %v", network.id, err)
		}
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	// The device reports both instances, and records every Retain it receives.
	var (
		mu      sync.Mutex
		retains [][]string
	)
	keepID := centralInstanceID("net-keep", machine)
	dropID := centralInstanceID("net-drop", machine)
	conn := connectDeviceWith(t, manager, "xena", machine, []string{keepID, dropID}, func(ids []string) {
		mu.Lock()
		retains = append(retains, ids)
		mu.Unlock()
	})
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool {
		me, err := store.GetUserByName(ctx, "xena")
		return err == nil && len(manager.Snapshots(me.ID)) == 1
	})
	_ = keepID

	// Remove the membership of the second network.
	me, err := store.GetUserByName(ctx, "xena")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	members, err := store.ListMembers(ctx, me.ID, "net-drop")
	if err != nil || len(members) != 1 {
		t.Fatalf("members of net-drop: %v %v", members, err)
	}
	if err := store.RemoveMember(ctx, me.ID, "net-drop", members[0].ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Instances: manager, Logf: func(string, ...any) {},
	}
	report, err := reconciler.ReconcileUser(ctx, me.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Stopped != 1 {
		t.Fatalf("stopped = %d, want 1", report.Stopped)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(retains) == 0 {
		t.Fatalf("no Retain was sent: the orphaned instance keeps running")
	}
	last := retains[len(retains)-1]
	// Retain carries the instances to KEEP: the surviving network only.
	if len(last) != 1 || last[0] != keepID {
		t.Fatalf("Retain kept %v, want exactly the surviving instance %s (dropped %s)", last, keepID, dropID)
	}
}

// centralInstanceID mirrors the id the console derives for a membership.
func centralInstanceID(networkID, machineID string) string {
	return central.InstanceID(networkID, machineID)
}

// connectDeviceWith opens a device channel that answers RunNetworkInstance and
// RetainNetworkInstance, reporting the given instances in its heartbeat.
func connectDeviceWith(t *testing.T, manager *session.Manager, username, machineID string,
	instances []string, onRetain func([]string)) *deviceConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		cancel()
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
		cancel()
		t.Fatalf("dial: %v", err)
	}
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		cancel()
		t.Fatalf("register run: %v", err)
	}
	if err := registry.Register(central.WebClientKey, central.MethodRetainNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			request := &manage.RetainNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, request); err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(request.GetInstIds()))
			for _, id := range request.GetInstIds() {
				ids = append(ids, formatUUID(id))
			}
			if onRetain != nil {
				onRetain(ids)
			}
			return proto.Marshal(&manage.RetainNetworkInstanceResponse{})
		}); err != nil {
		cancel()
		t.Fatalf("register retain: %v", err)
	}
	mux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	heartbeat := &web.HeartbeatRequest{
		UserToken: username,
		MachineId: mustUUID(t, machineID),
		Hostname:  "node",
	}
	for _, instance := range instances {
		heartbeat.RunningNetworkInstances = append(heartbeat.RunningNetworkInstances, mustUUID(t, instance))
	}
	if _, err := mux.Call(ctx, session.WebServerKey, 1, heartbeat); err != nil {
		cancel()
		t.Fatalf("heartbeat: %v", err)
	}
	return &deviceConn{mux: mux, cancel: cancel}
}

func formatUUID(id *common.UUID) string {
	if id == nil {
		return ""
	}
	raw := make([]byte, 16)
	binary.BigEndian.PutUint32(raw[0:4], id.GetPart1())
	binary.BigEndian.PutUint32(raw[4:8], id.GetPart2())
	binary.BigEndian.PutUint32(raw[8:12], id.GetPart3())
	binary.BigEndian.PutUint32(raw[12:16], id.GetPart4())
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// TestForeignInstanceIsLeftAlone is the safety half of F-086: a device may run
// instances from its own config, and the console must never stop those.
func TestForeignInstanceIsLeftAlone(t *testing.T) {
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
	user, err := store.CreateUser(ctx, "yuri", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "abcd2222-0000-4000-8000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidr := "10.92.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-only", DisplayName: "only", NetworkName: "only",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	if err := store.AddMember(ctx, bal.NetworkMember{
		UserID: user.ID, NetworkID: "net-only", ID: "member-only", DeviceID: machine,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("member: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	var (
		mu      sync.Mutex
		retains int
	)
	keepID := central.InstanceID("net-only", machine)
	localID := "99999999-0000-4000-8000-000000000001"
	conn := connectDeviceWith(t, manager, "yuri", machine, []string{keepID, localID}, func([]string) {
		mu.Lock()
		retains++
		mu.Unlock()
	})
	defer conn.close()
	waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Instances: manager, Logf: func(string, ...any) {},
	}
	report, err := reconciler.ReconcileUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Stopped != 0 {
		t.Fatalf("stopped = %d, want 0", report.Stopped)
	}
	mu.Lock()
	defer mu.Unlock()
	if retains != 0 {
		t.Fatalf("console sent %d Retain(s) while the device runs a local instance", retains)
	}
}

// TestSoakDeviceChurn runs many reconcile rounds while one device keeps
// reconnecting. The invariants: the configuration converges (no repeated
// pushes), no compile errors appear, and nothing accumulates.
func TestSoakDeviceChurn(t *testing.T) {
	rounds := 15
	if value := os.Getenv("EASYTIER_SOAK_ROUNDS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("EASYTIER_SOAK_ROUNDS: %v", err)
		}
		if parsed < 1 {
			t.Fatalf("EASYTIER_SOAK_ROUNDS must be at least 1, got %d", parsed)
		}
		rounds = parsed
	}
	// The budget has to follow the round count, otherwise a long run is cut
	// off by the context and reported as a convergence failure (round 160).
	soakBudget := time.Duration(rounds) * 2 * time.Second
	if soakBudget < 3*time.Minute {
		soakBudget = 3 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), soakBudget)
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
	user, err := store.CreateUser(ctx, "soak", hash)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machine := "abcd4444-0000-4000-8000-000000000001"
	if err := store.UpsertDevice(ctx, bal.Device{
		UserID: user.ID, MachineID: machine, Hostname: "node", FirstSeen: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	cidrA, cidrB := "10.94.0.0/29", "10.95.0.0/29"
	for index, network := range []struct {
		id   string
		cidr string
	}{{"soak-a", cidrA}, {"soak-b", cidrB}} {
		if err := store.CreateNetwork(ctx, bal.Network{
			UserID: user.ID, ID: network.id, DisplayName: network.id, NetworkName: network.id,
			NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &network.cidr,
			CreateTime: now.Add(time.Duration(index) * time.Second), UpdateTime: now,
		}); err != nil {
			t.Fatalf("network: %v", err)
		}
		if err := store.AddMember(ctx, bal.NetworkMember{
			UserID: user.ID, NetworkID: network.id, ID: "member-" + network.id, DeviceID: machine,
			CreateTime: now, UpdateTime: now,
		}); err != nil {
			t.Fatalf("member: %v", err)
		}
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	reconciler := &central.Reconciler{
		Store: store, Sessions: manager, Instances: manager, Logf: func(string, ...any) {},
	}

	instances := []string{
		central.InstanceID("soak-a", machine),
		central.InstanceID("soak-b", machine),
	}
	runtime.GC()
	baseline := runtime.NumGoroutine()

	pushedAfterFirst := 0
	for round := 0; round < rounds; round++ {
		conn := connectDeviceWith(t, manager, "soak", machine, instances, nil)
		waitFor(t, 5*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 1 })

		report, err := reconciler.ReconcileUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("round %d: reconcile: %v", round, err)
		}
		if report.CompileErrors != 0 {
			t.Fatalf("round %d: compile errors = %d", round, report.CompileErrors)
		}
		if report.Stopped != 0 {
			t.Fatalf("round %d: stopped = %d, want 0 (instances match the intent)", round, report.Stopped)
		}
		if round == rounds/2 {
			// Change one intent mid-soak: exactly one push must follow, then it
			// has to converge again.
			network, err := store.GetNetwork(ctx, user.ID, "soak-b")
			if err != nil {
				t.Fatalf("round %d: load network: %v", round, err)
			}
			network.NetworkName = "soak-b-renamed"
			if err := store.UpdateNetwork(ctx, network); err != nil {
				t.Fatalf("round %d: update network: %v", round, err)
			}
			changed, err := reconciler.ReconcileUser(ctx, user.ID)
			if err != nil {
				t.Fatalf("round %d: reconcile after change: %v", round, err)
			}
			if changed.Pushed != 1 {
				t.Fatalf("round %d: after changing one network pushed = %d, want 1", round, changed.Pushed)
			}
		}
		if round > 0 {
			pushedAfterFirst += report.Pushed
		}

		// Drop the tunnel; the session must be released before the next round.
		conn.close()
		waitFor(t, 10*time.Second, func() bool { return len(manager.Snapshots(user.ID)) == 0 })
	}

	if pushedAfterFirst != 0 {
		t.Fatalf("re-pushed %d time(s) after the first round: the config does not converge", pushedAfterFirst)
	}

	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > baseline+5 {
		t.Fatalf("goroutines grew from %d to %d during churn", baseline, after)
	}
}
