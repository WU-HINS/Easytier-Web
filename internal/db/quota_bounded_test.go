package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// TestAddMembersBoundedEnforcesTheQuota covers F-140 at the storage layer: the
// limit has to be checked inside the same transaction that inserts, otherwise two
// concurrent requests both pass a handler level check. This exercises the path
// deterministically instead of hoping the race fires.
func TestAddMembersBoundedEnforcesTheQuota(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	now := time.Now().UTC().Truncate(time.Second)

	user, err := store.CreateUser(ctx, "quota-user", "hash")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	cidr := "10.77.0.0/29"
	if err := store.CreateNetwork(ctx, bal.Network{
		UserID: user.ID, ID: "net-q", DisplayName: "q", NetworkName: "q-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	member := func(index int) bal.NetworkMember {
		return bal.NetworkMember{
			UserID: user.ID, NetworkID: "net-q", ID: fmt.Sprintf("m-%d", index),
			DeviceID:   fmt.Sprintf("dev-%d", index),
			CreateTime: now, UpdateTime: now,
		}
	}
	for index := 0; index < 3; index++ {
		machine := fmt.Sprintf("dev-%d", index)
		if err := store.UpsertDevice(ctx, bal.Device{
			UserID: user.ID, MachineID: machine, Hostname: machine, FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device %d: %v", index, err)
		}
	}

	// A batch that would overshoot the allowance is refused as a whole.
	err = store.AddMembersBounded(ctx, user.ID, []bal.NetworkMember{member(0), member(1), member(2)}, 2)
	if !errors.Is(err, bal.ErrQuotaExceeded) {
		t.Fatalf("batch over quota = %v, want ErrQuotaExceeded", err)
	}
	count, err := store.CountMembersByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("refused batch left %d members behind", count)
	}

	// Exactly at the limit is fine; one more is refused.
	if err := store.AddMembersBounded(ctx, user.ID, []bal.NetworkMember{member(0), member(1)}, 2); err != nil {
		t.Fatalf("fill to the quota: %v", err)
	}
	if err := store.AddMembersBounded(ctx, user.ID, []bal.NetworkMember{member(2)}, 2); !errors.Is(err, bal.ErrQuotaExceeded) {
		t.Fatalf("one over the quota = %v, want ErrQuotaExceeded", err)
	}

	// Quota 0 means unlimited, which the settings document says.
	if err := store.AddMembersBounded(ctx, user.ID, []bal.NetworkMember{member(2)}, 0); err != nil {
		t.Fatalf("unlimited quota refused: %v", err)
	}
}
