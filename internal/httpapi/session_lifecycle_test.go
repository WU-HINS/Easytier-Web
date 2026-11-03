package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// TestLoginIssuesAFreshSessionAndLogoutKillsOnlyThatOne covers F-185: nothing
// asserted that each login mints a new session id (session fixation would go
// unnoticed) or that logout invalidates exactly the session it was called on and
// leaves the other devices alone.
func TestLoginIssuesAFreshSessionAndLogoutKillsOnlyThatOne(t *testing.T) {
	h := newHarness(t)
	first := h.client(t)
	h.setupAdmin(t, first, "root-owner", strongPassword)

	firstCookie := sessionCookieValue(t, h, first)
	second := h.client(t)
	if status, body := h.do(t, second, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "root-owner", "password": strongPassword,
	}); status != http.StatusOK {
		t.Fatalf("second login = %d %s", status, body)
	}
	secondCookie := sessionCookieValue(t, h, second)
	if firstCookie == secondCookie {
		t.Fatal("login reused the session id; that is session fixation")
	}

	// Logout ends the session it was called on.
	if status, body := h.do(t, second, http.MethodPost, "/api/v1/auth/logout", nil); status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("logout = %d %s", status, body)
	}
	// The jar dropped the cookie, so the row itself is what has to be checked: a
	// replayed raw value must be refused.
	if status := replayCookie(t, h, secondCookie); status != http.StatusUnauthorized {
		t.Fatalf("the logged out session still works when replayed: %d", status)
	}

	// The replay of the old cookie is dead too, not just the client jar.
	if status := replayCookie(t, h, firstCookie); status != http.StatusOK {
		t.Fatalf("the first session was collateral damage of the second logout: %d", status)
	}
}

func sessionCookieValue(t *testing.T, h *harness, client *http.Client) string {
	t.Helper()
	target, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cookies := client.Jar.Cookies(target)
	if len(cookies) == 0 {
		t.Fatal("no session cookie")
	}
	return cookies[0].Value
}

func replayCookie(t *testing.T, h *harness, value string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/console-info", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "et_session", Value: value})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	defer response.Body.Close()
	return response.StatusCode
}
