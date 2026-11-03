package db_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestOIDCUpsertPicksAFreeUsername covers F-141: the free-name search used to be
// a read-then-write loop in the handler, so two first time SSO logins sharing a
// preferred_username both picked the same name and one failed on the unique
// index. The retry now lives in the store, next to the constraint.
func TestOIDCUpsertPicksAFreeUsername(t *testing.T) {
	ctx := context.Background()
	target := freshTarget(t)
	store := target.store
	if _, err := store.CreateUser(ctx, "alice", "hash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	const identities = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, identities)
	names := make([]string, identities)
	for index := 0; index < identities; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			user, err := store.UpsertOIDCUser(ctx, "https://idp.example",
				fmt.Sprintf("subject-%d", slot), "alice", "alice@example.com", "Alice")
			errs[slot] = err
			names[slot] = user.Username
		}(index)
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for index := 0; index < identities; index++ {
		if errs[index] != nil {
			t.Fatalf("identity %d failed: %v", index, errs[index])
		}
		if names[index] == "alice" {
			t.Fatalf("identity %d took the existing username", index)
		}
		if names[index] == "" {
			t.Fatalf("identity %d has no username", index)
		}
		if seen[names[index]] {
			t.Fatalf("duplicate username %q", names[index])
		}
		seen[names[index]] = true
	}

	// Repeated login of the same identity must return the same account.
	first, err := store.UpsertOIDCUser(ctx, "https://idp.example", "subject-0", "alice", "alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if first.Username != names[0] {
		t.Fatalf("second login returned %q, first was %q", first.Username, names[0])
	}
}
