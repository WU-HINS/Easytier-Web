package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRouteChangesAUsername covers the immutability promise made at OIDC
// registration: the name is chosen once. If someone later adds a rename route the
// promise silently disappears, so the absence of one is asserted here instead of
// being assumed (goal round 2).
func TestNoRouteChangesAUsername(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"PATCH /api/v1/users",
		"PUT /api/v1/users",
		"PATCH /api/v1/account/username",
		"POST /api/v1/account/username",
	}
	found := 0
	err := filepath.Walk(filepath.Join(root, "internal", "httpapi"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(body)
		for _, route := range forbidden {
			if strings.Contains(source, route) {
				t.Errorf("%s registers %q; usernames are immutable after registration", filepath.Base(path), route)
				found++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found > 0 {
		t.Fatalf("%d rename route(s) found", found)
	}
}
