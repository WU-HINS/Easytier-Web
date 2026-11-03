package session

import (
	"testing"

	"github.com/easytier/easytier-console/internal/rpc"
)

func newSession(userID int64, machine string) (*Session, *countingTunnel) {
	tunnel := &countingTunnel{}
	session := &Session{UserID: userID, MachineID: machine}
	session.mux = rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{})
	return session, tunnel
}

// TestDisconnectUserClosesEverySessionOfThatTenant covers F-003: deleting an
// account has to tear down its live tunnels, but only its own. Nothing exercised
// this before, so the "immediate" half of the revocation design was unverified.
func TestDisconnectUserClosesEverySessionOfThatTenant(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	first, firstTunnel := newSession(7, "m-1")
	second, secondTunnel := newSession(7, "m-2")
	other, otherTunnel := newSession(9, "m-3")
	manager.register(first)
	manager.register(second)
	manager.register(other)

	if firstTunnel.closes.Load() != 0 || secondTunnel.closes.Load() != 0 || otherTunnel.closes.Load() != 0 {
		t.Fatalf("registering already closed a tunnel (%d/%d/%d); the baseline below would prove nothing",
			firstTunnel.closes.Load(), secondTunnel.closes.Load(), otherTunnel.closes.Load())
	}

	if stopped := manager.DisconnectUser(7); stopped != 2 {
		t.Fatalf("DisconnectUser = %d, want 2", stopped)
	}
	if firstTunnel.closes.Load() != 1 || secondTunnel.closes.Load() != 1 {
		t.Fatalf("tenant tunnels closed %d/%d times, want 1/1", firstTunnel.closes.Load(), secondTunnel.closes.Load())
	}
	if otherTunnel.closes.Load() != 0 {
		t.Fatalf("another tenant's tunnel was closed %d time(s)", otherTunnel.closes.Load())
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("sessions left = %d, want 1", len(manager.sessions))
	}
}

// TestDisconnectDeviceClosesOnlyThatMachine covers the block-device path: one
// machine goes away, its siblings keep their tunnels.
func TestDisconnectDeviceClosesOnlyThatMachine(t *testing.T) {
	manager := &Manager{sessions: map[string]*Session{}}

	target, targetTunnel := newSession(7, "m-1")
	sibling, siblingTunnel := newSession(7, "m-2")
	manager.register(target)
	manager.register(sibling)

	if stopped := manager.DisconnectDevice(7, "m-1"); stopped != 1 {
		t.Fatalf("DisconnectDevice = %d, want 1", stopped)
	}
	if targetTunnel.closes.Load() != 1 {
		t.Fatalf("the blocked device's tunnel was closed %d time(s), want 1", targetTunnel.closes.Load())
	}
	if siblingTunnel.closes.Load() != 0 {
		t.Fatalf("a sibling tunnel was closed %d time(s)", siblingTunnel.closes.Load())
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("sessions left = %d, want 1", len(manager.sessions))
	}
	if stopped := manager.DisconnectDevice(9, "m-1"); stopped != 0 {
		t.Fatalf("another tenant's device disconnected %d session(s), want 0", stopped)
	}
}
