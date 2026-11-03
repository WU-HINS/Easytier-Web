package httpapi

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestLoginLimiterThrottlesPerAccountAndAddress covers F-145: the throttle is a
// security control with no tests at all, so its behaviour (per account, per
// address, reset on success, expiry) was a claim rather than a fact.
func TestLoginLimiterThrottlesPerAccountAndAddress(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	limiter := newLoginLimiter(3, time.Minute, nil)
	limiter.now = func() time.Time { return clock }
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.9:5555"

	for attempt := 0; attempt < 3; attempt++ {
		if ok, _ := limiter.allow(request, "alice"); !ok {
			t.Fatalf("attempt %d was throttled too early", attempt)
		}
		limiter.fail(request, "alice")
	}
	ok, wait := limiter.allow(request, "alice")
	if ok {
		t.Fatal("the fourth attempt was allowed")
	}
	if wait <= 0 || wait > time.Minute {
		t.Fatalf("retry after = %v, want within the window", wait)
	}

	// Another account from the same address is unaffected (the address bucket is
	// much wider than the per account limit).
	if ok, _ := limiter.allow(request, "bob"); !ok {
		t.Fatal("a different account was throttled by alice failures")
	}

	// Another address is unaffected, so one client cannot lock a victim out.
	other := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	other.RemoteAddr = "198.51.100.7:1234"
	if ok, _ := limiter.allow(other, "alice"); !ok {
		t.Fatal("an unrelated address inherited the throttle")
	}

	// A successful login clears the window.
	limiter.reset(request, "alice")
	if ok, _ := limiter.allow(request, "alice"); !ok {
		t.Fatal("reset did not clear the throttle")
	}

	// The window expires on its own.
	for attempt := 0; attempt < 3; attempt++ {
		limiter.fail(request, "alice")
	}
	if ok, _ := limiter.allow(request, "alice"); ok {
		t.Fatal("the window did not close")
	}
	clock = clock.Add(61 * time.Second)
	if ok, _ := limiter.allow(request, "alice"); !ok {
		t.Fatal("access was not restored after the window")
	}
}

// TestLoginLimiterDropsExpiredWindows covers F-144: sweep existed but nothing
// called it, so every distinct (address, username) pair stayed in the map for
// ever. The map has to stay bounded across a window boundary.
func TestLoginLimiterDropsExpiredWindows(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	limiter := newLoginLimiter(3, time.Minute, nil)
	limiter.now = func() time.Time { return clock }
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.9:5555"

	for index := 0; index < 400; index++ {
		limiter.fail(request, "user-"+strconv.Itoa(index))
	}
	if len(limiter.attempts) < 400 {
		t.Fatalf("the test did not fill the map: %d entries", len(limiter.attempts))
	}
	clock = clock.Add(2 * time.Minute)
	limiter.allow(request, "someone")
	if len(limiter.attempts) > 2 {
		t.Fatalf("expired windows were kept: %d entries left", len(limiter.attempts))
	}
}
