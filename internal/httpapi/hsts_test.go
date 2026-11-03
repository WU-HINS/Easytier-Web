package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestHSTSOnlyOnSecureTransport covers F-175: without Strict-Transport-Security a
// browser will happily follow a plain http link into the console, which is exactly
// what an SSL stripping attacker needs. The header must appear where TLS is in
// play and stay away from plain http deployments, which browsers ignore anyway
// but operators would rightly find surprising.
func TestHSTSOnlyOnSecureTransport(t *testing.T) {
	secure := newTLSHarness(t)
	response, err := secure.Client().Get(secure.URL + "/healthz")
	if err != nil {
		t.Fatalf("tls healthz: %v", err)
	}
	defer response.Body.Close()
	hsts := response.Header.Get("Strict-Transport-Security")
	if !strings.HasPrefix(hsts, "max-age=") {
		t.Fatalf("HSTS over TLS = %q, want a max-age directive", hsts)
	}
	if strings.Contains(hsts, "includeSubDomains") {
		t.Fatalf("HSTS must not claim subdomains by default: %q", hsts)
	}

	plain := newHarness(t)
	plainResponse, err := http.Get(plain.server.URL + "/healthz")
	if err != nil {
		t.Fatalf("plain healthz: %v", err)
	}
	defer plainResponse.Body.Close()
	if plainHSTS := plainResponse.Header.Get("Strict-Transport-Security"); plainHSTS != "" {
		t.Fatalf("HSTS over plain http = %q, want none", plainHSTS)
	}
}
