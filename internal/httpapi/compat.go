package httpapi

import (
	"net/http"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// The original console exposes a handful of routes this rewrite had dropped. Each
// one here is thin on purpose: it either answers from the session store directly
// or delegates to the handler that already owns the behaviour, so the two paths
// cannot drift apart.

// handleCheckLoginStatus answers the poll the original frontend does before it
// picks between the login page and the console.
func (s *Server) handleCheckLoginStatus(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
		return
	}
	sess, err := s.cfg.Store.GetSession(r.Context(), cookie.Value)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
		return
	}
	user, err := s.cfg.Store.GetUserByID(r.Context(), sess.UserID)
	if err != nil || user.Disabled {
		writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in": true,
		"user_name": user.Username,
		"is_admin":  user.Role == "admin",
	})
}

// handleCaptcha keeps the original path alive. This console has no captcha, and
// saying so is the contract: a client that reads enabled=false renders no captcha
// instead of waiting for an image that never arrives.
func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

// handleLogoutCompat accepts the GET the original frontend uses for logging out.
func (s *Server) handleLogoutCompat(w http.ResponseWriter, r *http.Request) {
	s.handleLogout(w, r)
}

// handleChangePasswordCompat accepts the original PUT on /auth/password.
func (s *Server) handleChangePasswordCompat(w http.ResponseWriter, r *http.Request, user bal.User) {
	s.handleChangePassword(w, r, user)
}

// handleListBlockedDevicesCompat serves the original /blocked-devices path.
func (s *Server) handleListBlockedDevicesCompat(w http.ResponseWriter, r *http.Request, user bal.User) {
	s.handleListBlockedDevices(w, r, user)
}

// handleUnblockDeviceCompat serves the original /blocked-devices/{id} path.
func (s *Server) handleUnblockDeviceCompat(w http.ResponseWriter, r *http.Request, user bal.User) {
	s.handleUnblockDevice(w, r, user)
}
