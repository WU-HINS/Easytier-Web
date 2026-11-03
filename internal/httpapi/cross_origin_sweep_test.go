package httpapi_test

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestCrossOriginGuardCoversEveryMutatingRoute covers F-176: the origin guard was
// only ever exercised on /auth/login. Routes are derived from the router source so
// a new write endpoint cannot quietly live outside the guard.
func TestCrossOriginGuardCoversEveryMutatingRoute(t *testing.T) {
	routes := mutatingRoutes(t)
	if len(routes) < 20 {
		t.Fatalf("only %d mutating routes found; the scan is not reading the router", len(routes))
	}

	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	host := strings.TrimPrefix(h.server.URL, "http://")

	for _, route := range routes {
		path := fillPlaceholders(route.path)
		body, status := sendWithOrigin(t, admin, route.method, h.server.URL+path, "http://evil.example")
		if status != http.StatusForbidden {
			t.Fatalf("%s %s with a foreign Origin = %d %s, want 403", route.method, route.path, status, body)
		}
		sameBody, sameStatus := sendWithOrigin(t, admin, route.method, h.server.URL+path, "http://"+host)
		if sameStatus == http.StatusForbidden && strings.Contains(sameBody, "cross-origin") {
			t.Fatalf("%s %s rejected its own origin: %s", route.method, route.path, sameBody)
		}
	}
}

type mutatingRoute struct {
	method string
	path   string
}

// mutatingRoutes reads registered routes out of the router source: the mux is not
// exported, and the source is the thing that changes when a route is added.
func mutatingRoutes(t *testing.T) []mutatingRoute {
	t.Helper()
	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	routes := []mutatingRoute{}
	for _, line := range strings.Split(string(source), "\n") {
		at := strings.Index(line, `HandleFunc("`)
		if at < 0 {
			continue
		}
		rest := line[at+len(`HandleFunc("`):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			continue
		}
		parts := strings.SplitN(rest[:end], " ", 2)
		if len(parts) != 2 || parts[0] == "GET" || !strings.HasPrefix(parts[1], "/api/v1") {
			continue
		}
		routes = append(routes, mutatingRoute{method: parts[0], path: parts[1]})
	}
	return routes
}

// fillPlaceholders swaps a braced parameter for a syntactically valid id.
func fillPlaceholders(path string) string {
	out := strings.Builder{}
	for index := 0; index < len(path); index++ {
		if path[index] == '{' {
			for index < len(path) && path[index] != '}' {
				index++
			}
			out.WriteString("11111111-1111-4111-8111-111111111111")
			continue
		}
		out.WriteByte(path[index])
	}
	return out.String()
}

func sendWithOrigin(t *testing.T, client *http.Client, method, url, origin string) (string, int) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("request %s %s: %v", method, url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	payload := make([]byte, 256)
	read, _ := response.Body.Read(payload)
	return string(payload[:read]), response.StatusCode
}
