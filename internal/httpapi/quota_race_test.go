package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/settings"
)

// TestConcurrentMemberAddsRespectQuota covers F-140: the quota check counted the
// existing memberships and then inserted, so on PostgreSQL two concurrent adds
// could both pass the check and push the tenant over its allowance. SQLite
// never showed it because writes are serialised.
func TestConcurrentMemberAddsRespectQuota(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "ivy", strongPassword)
	me, err := h.store.GetUserByName(ctx, "ivy")
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	const quota = 2
	const devices = 6
	if err := h.store.PutSetting(ctx, settings.KeyMaxInstances, fmt.Sprintf("%d", quota)); err != nil {
		t.Fatalf("quota setting: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for index := 0; index < devices; index++ {
		machine := fmt.Sprintf("beef9000-0000-4000-8000-%012d", index)
		if err := h.store.UpsertDevice(ctx, bal.Device{
			UserID: me.ID, MachineID: machine, Hostname: fmt.Sprintf("node-%d", index),
			FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device %d: %v", index, err)
		}
	}
	cidr := "10.66.0.0/29"
	network := "net-quota"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: network, DisplayName: "quota", NetworkName: "quota-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, devices)
	for index := 0; index < devices; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			machine := fmt.Sprintf("beef9000-0000-4000-8000-%012d", slot)
			status, _, err := doJSONNoFatal(h.client(t), http.MethodPost,
				h.server.URL+"/api/v1/networks/"+network+"/members",
				map[string]any{"device_ids": []string{machine}})
			if err != nil {
				t.Errorf("add member %d: %v", slot, err)
				return
			}
			codes[slot] = status
		}(index)
	}
	close(start)
	wg.Wait()

	members, err := h.store.ListMembers(ctx, me.ID, network)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) > quota {
		t.Fatalf("members = %d, quota = %d, statuses = %v", len(members), quota, codes)
	}
	// The quota is enforced inside the transaction, so the request that loses the
	// race has to come back as a conflict, not as a server error (r134).
	for slot, status := range codes {
		if status == 500 {
			t.Fatalf("writer %d got 500 under quota pressure; statuses = %v", slot, codes)
		}
	}
}

// TestQuotaRejectionInsideTheTransactionIsAConflict drives the branch that the
// bulk race above only reaches by luck: two writers both pass the handler level
// pre-check because one slot is free, one of them wins the row lock, and the
// loser is rejected by the bounded insert. That loser must be a 409, not a 500
// (found by the review team in r134).
func TestQuotaRejectionInsideTheTransactionIsAConflict(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "juno", strongPassword)
	me, err := h.store.GetUserByName(ctx, "juno")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	const quota = 2
	if err := h.store.PutSetting(ctx, settings.KeyMaxInstances, fmt.Sprintf("%d", quota)); err != nil {
		t.Fatalf("quota setting: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	machines := []string{"beef9100-0000-4000-8000-000000000001", "beef9100-0000-4000-8000-000000000002", "beef9100-0000-4000-8000-000000000003"}
	for index, machine := range machines {
		if err := h.store.UpsertDevice(ctx, bal.Device{
			UserID: me.ID, MachineID: machine, Hostname: fmt.Sprintf("node-%d", index),
			FirstSeen: now, LastSeen: now,
		}); err != nil {
			t.Fatalf("device %d: %v", index, err)
		}
	}
	cidr := "10.67.0.0/29"
	if err := h.store.CreateNetwork(ctx, bal.Network{
		UserID: me.ID, ID: "net-race", DisplayName: "race", NetworkName: "race-net",
		NetworkSecret: "s", NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		CreateTime: now, UpdateTime: now,
	}); err != nil {
		t.Fatalf("network: %v", err)
	}
	// Fill all but one slot so both writers can pass the pre-check.
	status, body := h.do(t, user, http.MethodPost, "/api/v1/networks/net-race/members", map[string]any{
		"device_ids": []string{machines[0]},
	})
	if status != http.StatusNoContent {
		t.Fatalf("seed member = %d %s", status, body)
	}

	codes := make([]int, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			code, _ := h.do(t, user, http.MethodPost, "/api/v1/networks/net-race/members", map[string]any{
				"device_ids": []string{machines[slot+1]},
			})
			codes[slot] = code
		}(index)
	}
	close(start)
	wg.Wait()

	for slot, code := range codes {
		if code == http.StatusInternalServerError {
			t.Fatalf("writer %d got 500 when the transaction rejected it; statuses = %v", slot, codes)
		}
	}
	winners := 0
	for _, code := range codes {
		if code == http.StatusNoContent {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1; statuses = %v", winners, codes)
	}
}
