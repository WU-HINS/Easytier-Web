package httpapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// TestConcurrentRegistrationKeepsOneAccount covers F-142: registration checked
// whether a name was free and then inserted, so two racing sign ups could both
// pass the check. The loser used to get 500 with driver text; it must be a plain
// 409 and exactly one account must exist.
func TestConcurrentRegistrationKeepsOneAccount(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	const racers = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	statuses := make([]int, racers)
	bodies := make([]string, racers)
	for index := 0; index < racers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			status, body, err := doJSONNoFatal(h.client(t), http.MethodPost,
				h.server.URL+"/api/v1/auth/register", map[string]string{
					"username": "race-user", "password": strongPassword,
				})
			if err != nil {
				t.Errorf("register %d: %v", slot, err)
				return
			}
			statuses[slot] = status
			bodies[slot] = string(body)
		}(index)
	}
	close(start)
	wg.Wait()

	created := 0
	conflicts := 0
	for index := 0; index < racers; index++ {
		switch statuses[index] {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("registration %d = %d %s", index, statuses[index], bodies[index])
		}
	}
	if created != 1 {
		t.Fatalf("created = %d, want exactly 1 (statuses %v)", created, statuses)
	}
	if conflicts != racers-1 {
		t.Fatalf("conflicts = %d, want %d (statuses %v)", conflicts, racers-1, statuses)
	}
	users, err := h.store.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	named := 0
	for _, user := range users {
		if user.Username == "race-user" {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("accounts named race-user = %d, want 1", named)
	}
}
