package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// TestRotationInvalidatesTheCallersOldCookie covers F-184: after a rotation the
// caller keeps working through the NEW cookie, but the cookie they presented must
// be dead. If the old token survived, a stolen cookie would outlive the very
// action taken to revoke it.
func TestRotationInvalidatesTheCallersOldCookie(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "ivy", strongPassword)

	target, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	cookies := user.Jar.Cookies(target)
	if len(cookies) == 0 {
		t.Fatal("no session cookie after registering")
	}
	oldValue := cookies[0].Value

	const newPassword = "R0tated-Strong-Pass!"
	if status, body := h.do(t, user, http.MethodPost, "/api/v1/account/password", map[string]string{
		"current_password": strongPassword, "new_password": newPassword,
	}); status != http.StatusNoContent {
		t.Fatalf("rotate = %d %s", status, body)
	}

	// The caller is still logged in through the reissued cookie.
	if status, _ := h.do(t, user, http.MethodGet, "/api/v1/console-info", nil); status != http.StatusOK {
		t.Fatalf("the caller lost their session on rotation")
	}

	// The cookie presented before the rotation must not work any more.
	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/console-info", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: cookies[0].Name, Value: oldValue})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the pre rotation cookie still works: %d", response.StatusCode)
	}
}
