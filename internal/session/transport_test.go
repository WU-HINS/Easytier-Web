package session

import (
	"testing"

	"github.com/easytier/easytier-console/internal/rpc"
)

func sessionFor(userID int64, machine, transport string) (*Session, *countingTunnel) {
	tunnel := &countingTunnel{}
	session := &Session{UserID: userID, MachineID: machine, Transport: transport}
	session.mux = rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	return session, tunnel
}

// TestOneDeviceOnThreeTransportsKeepsThreeSessions covers the decision that a
// device may report over ws, tcp and udp at the same time: the registry keys on
// the transport, so none of them evicts the others.
func TestOneDeviceOnThreeTransportsKeepsThreeSessions(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	tcpSession, tcpTunnel := sessionFor(7, "m-1", "tcp")
	wsSession, wsTunnel := sessionFor(7, "m-1", "ws")
	udpSession, udpTunnel := sessionFor(7, "m-1", "udp")
	manager.register(tcpSession)
	manager.register(wsSession)
	manager.register(udpSession)

	if got := len(manager.sessions); got != 3 {
		t.Fatalf("sessions = %d, want 3 (one per transport)", got)
	}
	for _, tunnel := range []*countingTunnel{tcpTunnel, wsTunnel, udpTunnel} {
		if tunnel.closes.Load() != 0 {
			t.Fatalf("registering a second transport closed another transport's tunnel")
		}
	}
}

// TestByMachinePrefersTCPThenWSThenUDP pins the single-push rule: the console
// configures a device once, so lookup has to be deterministic.
func TestByMachinePrefersTCPThenWSThenUDP(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	tcpSession, _ := sessionFor(7, "m-1", "tcp")
	wsSession, _ := sessionFor(7, "m-1", "ws")
	udpSession, _ := sessionFor(7, "m-1", "udp")
	// Register in the least favourable order: the result must not depend on it.
	manager.register(udpSession)
	manager.register(wsSession)
	manager.register(tcpSession)

	if got, ok := manager.ByMachine(7, "m-1"); !ok || got != tcpSession {
		t.Fatalf("ByMachine = %v, want the tcp session", got)
	}
	manager.forget(tcpSession)
	if got, ok := manager.ByMachine(7, "m-1"); !ok || got != wsSession {
		t.Fatalf("after tcp left, ByMachine = %v, want the ws session", got)
	}
	manager.forget(wsSession)
	if got, ok := manager.ByMachine(7, "m-1"); !ok || got != udpSession {
		t.Fatalf("after ws left, ByMachine = %v, want the udp session", got)
	}
	manager.forget(udpSession)
	if _, ok := manager.ByMachine(7, "m-1"); ok {
		t.Fatal("a device with no tunnel was still reported as online")
	}
}

// TestSameTransportStillReplaces keeps the r136 behaviour: a second tunnel on the
// same transport replaces the first one instead of leaving it running.
func TestSameTransportStillReplaces(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	first, firstTunnel := sessionFor(7, "m-1", "tcp")
	second, secondTunnel := sessionFor(7, "m-1", "tcp")
	manager.register(first)
	manager.register(second)

	if firstTunnel.closes.Load() != 1 {
		t.Fatalf("the replaced tunnel was closed %d time(s), want 1", firstTunnel.closes.Load())
	}
	if secondTunnel.closes.Load() != 0 {
		t.Fatal("the new tunnel was closed by its own registration")
	}
	if got := len(manager.sessions); got != 1 {
		t.Fatalf("sessions = %d, want 1", got)
	}
}
