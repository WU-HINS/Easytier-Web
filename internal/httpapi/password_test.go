package httpapi_test

import (
	"net/http"
	"testing"
)

// TestPasswordRotation covers F-135: there was no way for a user to change their
// own password, and the admin surface is deliberately read only, so a leaked
// password could never be rotated.
func TestPasswordRotation(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "hank", strongPassword)

	// A second browser for the same account, to prove rotation kills other sessions.
	other := h.client(t)
	if status, body := h.do(t, other, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "hank", "password": strongPassword,
	}); status != http.StatusOK {
		t.Fatalf("second login = %d %s", status, body)
	}

	const newPassword = "An0ther-Strong-Pass!"

	// Wrong current password is refused and does not change anything.
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/account/password", map[string]string{
		"current_password": "definitely-wrong", "new_password": newPassword,
	}); status != http.StatusUnauthorized {
		t.Fatalf("wrong current password = %d %s, want 401", status, body)
	}
	if status, _ := h.do(t, other, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusOK {
		t.Fatalf("the other session died on a failed attempt")
	}

	// Too weak is refused by the policy.
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/account/password", map[string]string{
		"current_password": strongPassword, "new_password": "short",
	}); status != http.StatusBadRequest {
		t.Fatalf("weak password = %d %s, want 400", status, body)
	}

	// The happy path.
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/account/password", map[string]string{
		"current_password": strongPassword, "new_password": newPassword,
	}); status != http.StatusNoContent {
		t.Fatalf("rotate = %d %s, want 204", status, body)
	}

	// The caller keeps working, every other session is gone.
	if status, _ := h.do(t, user, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusOK {
		t.Fatalf("the caller was logged out by their own rotation")
	}
	if status, _ := h.do(t, other, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusUnauthorized {
		t.Fatalf("another session survived the rotation")
	}

	// The old password no longer works, the new one does.
	fresh := h.client(t)
	if status, _ := h.do(t, fresh, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "hank", "password": strongPassword,
	}); status != http.StatusUnauthorized {
		t.Fatalf("the old password still works")
	}
	second := h.client(t)
	if status, body := h.do(t, second, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "hank", "password": newPassword,
	}); status != http.StatusOK {
		t.Fatalf("the new password does not work: %d %s", status, body)
	}

	// The admin still cannot set a password for someone else.
	if status, _ := h.do(t, admin, http.MethodPost, "/api/v1/admin/users/2/password", map[string]string{
		"new_password": newPassword,
	}); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Fatalf("an admin password endpoint exists: %d", status)
	}
}
