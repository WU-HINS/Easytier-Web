package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// R-E, r253: trusting X-Forwarded-For unconditionally lets a caller rotate the
// header and never hit the login throttle, and it puts an attacker-chosen address
// into the audit view. The decision lives in one function; this pins both halves.
func TestClientAddressHonoursTrustProxy(t *testing.T) {
	request := func(remote string, forwarded string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		r.RemoteAddr = remote
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		return r
	}

	if got := clientAddress(request("203.0.113.9:5555", "9.9.9.9"), false); got != "203.0.113.9" {
		t.Fatalf("without trust the peer must win, got %q", got)
	}
	if got := clientAddress(request("203.0.113.9:5555", "9.9.9.9"), true); got != "9.9.9.9" {
		t.Fatalf("with trust the header must win, got %q", got)
	}
	// Only the first hop counts: a client can append entries of its own.
	if got := clientAddress(request("203.0.113.9:5555", "198.51.100.7, 9.9.9.9"), true); got != "198.51.100.7" {
		t.Fatalf("only the first forwarded entry counts, got %q", got)
	}
	if got := clientAddress(request("203.0.113.9:5555", ""), true); got != "203.0.113.9" {
		t.Fatalf("without the header the peer must be used, got %q", got)
	}
}
