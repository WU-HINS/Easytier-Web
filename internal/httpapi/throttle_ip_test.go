package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestLoginThrottleIsPerAddressToo covers F-037: the per-account counter alone
// is bypassed by rotating usernames, and every attempt costs an argon2
// computation, so the address needs its own bucket.
func TestLoginThrottleIsPerAddressToo(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	client := h.client(t)
	last := 0
	for i := 0; i < 31; i++ {
		status, _ := h.do(t, client, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": fmt.Sprintf("nobody-%d", i), // a fresh account each time
			"password": "wrong-password",
		})
		if status != http.StatusUnauthorized && status != http.StatusTooManyRequests {
			t.Fatalf("attempt %d status = %d", i, status)
		}
		last = status
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("final status = %d, want 429 after rotating usernames", last)
	}
}
