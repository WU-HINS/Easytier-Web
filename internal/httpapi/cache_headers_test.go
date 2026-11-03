package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestApiResponsesAreNeverCached covers F-174: tenant data comes back to a cookie
// authenticated GET, and without an explicit Cache-Control a shared cache has no
// way to tell two sessions apart. Static assets keep their own policy, so this
// also checks that no-store is not applied to everything.
func TestApiResponsesAreNeverCached(t *testing.T) {
	h := newHarness(t)
	client := h.client(t)
	h.setupAdmin(t, client, "root-owner", strongPassword)

	response, err := client.Get(h.server.URL + "/api/v1/machines")
	if err != nil {
		t.Fatalf("machines: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("machines = %d", response.StatusCode)
	}
	if cache := response.Header.Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("API Cache-Control = %q, want no-store", cache)
	}
	if pragma := response.Header.Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("API Pragma = %q, want no-cache", pragma)
	}

	asset, err := http.Get(h.server.URL + "/boot.js")
	if err != nil {
		t.Fatalf("boot.js: %v", err)
	}
	defer asset.Body.Close()
	if cache := asset.Header.Get("Cache-Control"); strings.Contains(cache, "no-store") {
		t.Fatalf("static asset got no-store: %q", cache)
	}
}
