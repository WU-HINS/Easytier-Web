package central

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/rpc"
)

// fakeLister reports what each online device is running.
type fakeLister map[string][]string

func (f fakeLister) OnlineInstances(int64) map[string][]string { return f }

// recordingCaller records every retain call and can fail for chosen machines.
type recordingCaller struct {
	fail     map[string]bool
	machines []string
	retained map[string][]string
}

func (c *recordingCaller) Call(_ context.Context, _ int64, machineID string, _ rpc.ServiceKey, _ uint32, req proto.Message) ([]byte, error) {
	c.machines = append(c.machines, machineID)
	if c.fail[machineID] {
		return nil, errors.New("device unreachable")
	}
	retain, ok := req.(*manage.RetainNetworkInstanceRequest)
	if !ok {
		return nil, errors.New("unexpected request type")
	}
	if c.retained == nil {
		c.retained = map[string][]string{}
	}
	for _, id := range retain.InstIds {
		c.retained[machineID] = append(c.retained[machineID], id.String())
	}
	return nil, nil
}

const testUUID = "11111111-2222-3333-4444-555555555555"

// F-075: one unreachable device must not stop the pass for the others. The
// acceptance note said "EnforceQuota uses continue"; this pins it.
func TestQuotaKeepsGoingWhenOneDeviceFails(t *testing.T) {
	// Quota 4 keeps two instances on a and two on b; c is trimmed. machine-a is
	// the one that cannot be reached, and machine-b still has to be called.
	caller := &recordingCaller{fail: map[string]bool{"machine-a": true}}
	reconciler := &Reconciler{Sessions: caller}
	lister := fakeLister{
		"machine-a": {"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"},
		"machine-b": {"33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444"},
		"machine-c": {"55555555-5555-5555-5555-555555555555", "66666666-6666-6666-6666-666666666666"},
	}
	report, err := reconciler.EnforceQuota(context.Background(), 1, lister, 4)
	if err != nil {
		t.Fatalf("EnforceQuota: %v", err)
	}
	if report.Failed != 1 {
		t.Fatalf("failed = %d, want 1", report.Failed)
	}
	if report.Machines != 2 {
		t.Fatalf("machines visited = %d, want 2 (the pass stopped early)", report.Machines)
	}
	if len(caller.machines) != 2 || caller.machines[1] != "machine-b" {
		t.Fatalf("calls = %v, want machine-a then machine-b", caller.machines)
	}
	if len(caller.retained["machine-b"]) != 2 {
		t.Fatalf("machine-b retained %d instances, want 2", len(caller.retained["machine-b"]))
	}
}

// F-016: two passes over the same usage must retain the same instances, in the
// same order. If they did not, a device would flap in and out of the quota.
func TestQuotaRetentionIsDeterministicAcrossPasses(t *testing.T) {
	usage := fakeLister{
		"machine-b": {"33333333-2222-3333-4444-555555555555", "11111111-2222-3333-4444-555555555555"},
		"machine-a": {"22222222-2222-3333-4444-555555555555"},
	}

	first := &recordingCaller{}
	if _, err := (&Reconciler{Sessions: first}).EnforceQuota(context.Background(), 1, usage, 2); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second := &recordingCaller{}
	if _, err := (&Reconciler{Sessions: second}).EnforceQuota(context.Background(), 1, usage, 2); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if len(first.retained) != len(second.retained) {
		t.Fatalf("retained machines differ: %v vs %v", first.retained, second.retained)
	}
	for machine, ids := range first.retained {
		other, ok := second.retained[machine]
		if !ok {
			t.Fatalf("second pass did not retain on %s", machine)
		}
		if len(ids) != len(other) {
			t.Fatalf("%s: %d vs %d instances", machine, len(ids), len(other))
		}
		for index := range ids {
			if ids[index] != other[index] {
				t.Fatalf("%s: instance order changed between passes: %v vs %v", machine, ids, other)
			}
		}
	}
}
