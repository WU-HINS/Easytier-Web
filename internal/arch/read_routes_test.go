package arch

import (
	"path/filepath"
	"strings"
	"testing"
)

// publicReadRoutes is the complete list of GET routes that may answer without a
// session. Everything else has to be behind requireUser or requireAdmin.
var publicReadRoutes = []string{
	"/api/v1/setup",
	"/api/v1/auth/oidc/config",
	"/api/v1/auth/oidc/login",
	"/api/v1/auth/oidc/callback",
}

// TestReadRoutesRequireASession is the read side of F-170 and F-176: a GET route
// added without requireUser would expose tenant data to anyone who can reach the
// port, and nothing else in the suite enumerates routes for that property.
func TestReadRoutesRequireASession(t *testing.T) {
	root := repoRoot(t)
	router := mustReadFile(t, filepath.Join(root, "internal", "httpapi", "router.go"))
	public := map[string]bool{}
	for _, path := range publicReadRoutes {
		public[path] = true
	}
	marker := "HandleFunc(" + string('"')
	quote := string('"')
	checked := 0
	for _, line := range strings.Split(router, "\n") {
		trimmed := strings.TrimSpace(line)
		at := strings.Index(trimmed, marker)
		if at < 0 {
			continue
		}
		spec := trimmed[at+len(marker):]
		end := strings.Index(spec, quote)
		if end < 0 {
			continue
		}
		parts := strings.SplitN(spec[:end], " ", 2)
		if len(parts) != 2 || parts[0] != "GET" {
			continue
		}
		if !strings.HasPrefix(parts[1], "/api/v1") {
			continue
		}
		if public[parts[1]] {
			continue
		}
		checked++
		if !strings.Contains(trimmed, "s.requireUser(") && !strings.Contains(trimmed, "s.requireAdmin(") {
			t.Errorf("GET %s is registered without requireUser or requireAdmin", parts[1])
		}
	}
	if checked < 15 {
		t.Fatalf("only %d protected GET routes found; the scan is not reading the router", checked)
	}
}
