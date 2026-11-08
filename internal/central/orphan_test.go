package central

import (
	"context"
	"testing"

	"github.com/easytier/easytier-console/internal/pb/common"
)

// Deleting a network is a revocation, not just a row removal. A device that is
// online but no longer has any network must have its instance stopped on the next
// pass: the console created it, so the console is allowed to take it away.
func TestDeletingTheLastNetworkStopsTheInstance(t *testing.T) {
	caller := &recordingCaller{}
	reconciler := &Reconciler{
		Sessions:  caller,
		Instances: fakeLister{"machine-a": {testUUID}},
	}
	expected := map[string][]*common.UUID{} // nothing is expected any more
	managed := map[string]map[string]bool{"machine-a": {testUUID: true}}

	stopped := reconciler.stopOrphans(context.Background(), 1, expected, managed)
	if stopped != 1 {
		t.Fatalf("stopped = %d, want 1", stopped)
	}
	if len(caller.machines) != 1 || caller.machines[0] != "machine-a" {
		t.Fatalf("machines called = %v, want machine-a", caller.machines)
	}
	if kept := caller.retained["machine-a"]; len(kept) != 0 {
		t.Fatalf("the retain request kept %v, want an empty set", kept)
	}
}

// The console must not touch instances it did not create: a device may run its own
// configuration, and deleting a console network is not permission to stop that.
func TestForeignInstancesAreLeftAlone(t *testing.T) {
	caller := &recordingCaller{}
	reconciler := &Reconciler{
		Sessions:  caller,
		Instances: fakeLister{"machine-a": {"not-ours"}},
	}
	stopped := reconciler.stopOrphans(context.Background(), 1, map[string][]*common.UUID{}, map[string]map[string]bool{})
	if stopped != 0 {
		t.Fatalf("stopped = %d, want 0", stopped)
	}
	if len(caller.machines) != 0 {
		t.Fatalf("called %v, want no calls", caller.machines)
	}
}
