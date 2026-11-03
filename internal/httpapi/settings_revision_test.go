package httpapi_test

import (
	"net/http"
	"testing"
)

// TestSettingsRevisionConflict covers F-018: two admins editing at the same
// time must get a conflict instead of silently overwriting each other.
func TestSettingsRevisionConflict(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	type settingsResponse struct {
		Revision            int "json:\"revision\""
		MaxInstancesPerUser int "json:\"max_instances_per_user\""
	}
	read := func() settingsResponse {
		t.Helper()
		status, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/settings", nil)
		if status != http.StatusOK {
			t.Fatalf("get settings = %d %s", status, body)
		}
		var response settingsResponse
		if err := decodeInto(body, &response); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return response
	}

	start := read()
	if start.Revision != 0 {
		t.Fatalf("initial revision = %d, want 0", start.Revision)
	}

	status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"max_instances_per_user": 3,
		"expected_revision":      start.Revision,
	})
	if status != http.StatusOK {
		t.Fatalf("first write = %d %s", status, body)
	}
	var afterFirst settingsResponse
	if err := decodeInto(body, &afterFirst); err != nil {
		t.Fatalf("decode first write: %v", err)
	}
	if afterFirst.Revision != 1 {
		t.Fatalf("revision after first write = %d, want 1", afterFirst.Revision)
	}

	// Second writer still holds revision 0: it must be rejected, not merged.
	status, body = h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"max_instances_per_user": 99,
		"expected_revision":      0,
	})
	if status != http.StatusConflict {
		t.Fatalf("stale write = %d %s, want 409", status, body)
	}
	var conflict struct {
		Code     string "json:\"code\""
		Revision int    "json:\"revision\""
	}
	if err := decodeInto(body, &conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Code != "settings_conflict" || conflict.Revision != 1 {
		t.Fatalf("conflict payload = %+v", conflict)
	}

	// The rejected write must not have changed anything.
	if current := read(); current.MaxInstancesPerUser != 3 {
		t.Fatalf("quota after rejected write = %d, want 3", current.MaxInstancesPerUser)
	}

	// Re-reading and retrying succeeds.
	current := read()
	if status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"max_instances_per_user": 99,
		"expected_revision":      current.Revision,
	}); status != http.StatusOK {
		t.Fatalf("retry = %d %s", status, body)
	}
	if updated := read(); updated.MaxInstancesPerUser != 99 || updated.Revision != 2 {
		t.Fatalf("after retry = %+v", updated)
	}

	// A writer that omits the token still works (backwards compatible), but it
	// still bumps the revision so the next CAS caller is protected.
	if status, _ := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
		"extra_port": 21000,
	}); status != http.StatusOK {
		t.Fatalf("unconditional write = %d", status)
	}
	if final := read(); final.Revision != 3 {
		t.Fatalf("final revision = %d, want 3", final.Revision)
	}
}
