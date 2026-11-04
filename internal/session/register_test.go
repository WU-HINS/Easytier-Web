package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

type countingTunnel struct {
	closes atomic.Int32
}

func (t *countingTunnel) Send(context.Context, *wire.Packet) error { return nil }
func (t *countingTunnel) Recv(context.Context) (*wire.Packet, error) {
	return nil, context.Canceled
}
func (t *countingTunnel) RemoteAddr() string { return "test" }
func (t *countingTunnel) LocalAddr() string  { return "test" }
func (t *countingTunnel) Close() error {
	t.closes.Add(1)
	return nil
}

// TestRegisterClosesTheTunnelItReplaces covers F-218: a second tunnel for the same
// device used to overwrite the registry entry while the first connection stayed
// alive, so blocking the device only closed the newest one.
func TestRegisterClosesTheTunnelItReplaces(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	firstTunnel := &countingTunnel{}
	first := &Session{UserID: 7, MachineID: "m-1"}
	first.mux = rpc.NewMux(firstTunnel, rpc.NewRegistry(), rpc.Options{CallTimeout: 0})
	manager.register(first)

	secondTunnel := &countingTunnel{}
	second := &Session{UserID: 7, MachineID: "m-1"}
	second.mux = rpc.NewMux(secondTunnel, rpc.NewRegistry(), rpc.Options{CallTimeout: 0})
	manager.register(second)

	if firstTunnel.closes.Load() != 1 {
		t.Fatalf("the replaced tunnel was closed %d time(s), want 1", firstTunnel.closes.Load())
	}
	if secondTunnel.closes.Load() != 0 {
		t.Fatalf("the new tunnel was closed by its own registration")
	}
	manager.mu.RLock()
	current := manager.sessions[sessionKey(7, "m-1", "")]
	manager.mu.RUnlock()
	if current != second {
		t.Fatal("the registry no longer points at the newest session")
	}
}
