package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdminRoutesRequireTheAdminWrapper closes a gap the privilege matrix left:
// the matrix lists five endpoints explicitly, so a sixth admin route added without
// the wrapper would be public and no test would notice. The router source is the
// source of truth here.
func TestAdminRoutesRequireTheAdminWrapper(t *testing.T) {
	root := repoRoot(t)
	router := mustReadFile(t, filepath.Join(root, "internal", "httpapi", "router.go"))
	adminRoutes := 0
	for _, line := range strings.Split(router, "\n") {
		if !strings.Contains(line, "/api/v1/admin/") {
			continue
		}
		if !strings.Contains(line, "HandleFunc(") {
			continue
		}
		adminRoutes++
		if !strings.Contains(line, "s.requireAdmin(") {
			t.Errorf("admin route is not wrapped in requireAdmin: %s", strings.TrimSpace(line))
		}
	}
	if adminRoutes < 5 {
		t.Fatalf("only %d admin routes found; the scan is not reading the router", adminRoutes)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
