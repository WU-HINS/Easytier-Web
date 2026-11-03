package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestConcurrentSetupCreatesExactlyOneAdmin is the R-C collision case: the
// guard and the write must be a single statement, not count-then-insert.
func TestConcurrentSetupCreatesExactlyOneAdmin(t *testing.T) {
	h := newHarness(t)

	const racers = 8
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		status = map[int]int{}
		bodies = map[int]string{}
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			client := h.client(t)
			code, body, err := doJSONNoFatal(client, http.MethodPost, h.server.URL+"/api/v1/setup", map[string]string{
				"username": fmt.Sprintf("admin-candidate-%d", index),
				"password": strongPassword,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status[-1]++
				bodies[-1] = err.Error()
				return
			}
			status[code]++
			if _, ok := bodies[code]; !ok {
				bodies[code] = string(body)
			}
		}(i)
	}
	wg.Wait()

	if status[http.StatusCreated] != 1 {
		t.Fatalf("created = %d, want exactly 1; status distribution = %v bodies = %v", status[http.StatusCreated], status, bodies)
	}
	if status[http.StatusConflict] != racers-1 {
		t.Fatalf("conflicts = %d, want %d; status distribution = %v bodies = %v", status[http.StatusConflict], racers-1, status, bodies)
	}
	admins, err := h.store.CountUsersByRole(context.Background(), "admin")
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 1 {
		t.Fatalf("admin rows = %d, want 1", admins)
	}
}

func TestLoginIsThrottled(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	client := h.client(t)
	for attempt := 0; attempt < 10; attempt++ {
		status, _ := h.do(t, client, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "root-owner", "password": "wrong-password",
		})
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, status)
		}
	}
	status, body := h.do(t, client, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "root-owner", "password": "wrong-password",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d %s, want 429", status, body)
	}
}

// TestSecurityHeaders covers clickjacking and MIME sniffing (R-D).
func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	response, err := http.Get(h.server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer response.Body.Close()
	expect := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	}
	for header, want := range expect {
		if got := response.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	csp := response.Header.Get("Content-Security-Policy")
	for _, fragment := range []string{"default-src 'self'", "frame-ancestors 'none'", "script-src 'self'"} {
		if !strings.Contains(csp, fragment) {
			t.Fatalf("csp %q missing %q", csp, fragment)
		}
	}
}
