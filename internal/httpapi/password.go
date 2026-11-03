package httpapi

import (
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/logging"
)

var passwordLog = logging.New("ACCOUNT").Sub("Password")

// handleChangePassword lets a user rotate their own password. The admin surface
// stays read only (no way to set someone else password), so this is the only
// path, and it ends every other session of that account.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	if allowed, retryAfter := s.loginLimit.allow(r, user.Username); !allowed {
		w.Header().Set("Retry-After", strings.TrimSpace(retryAfter.String()))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	if err := verifyPassword(payload.CurrentPassword, user.PasswordHash); err != nil {
		s.loginLimit.fail(r, user.Username)
		passwordLog.Tag("DENY").Warn("wrong current password for " + user.Username)
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if payload.NewPassword == payload.CurrentPassword {
		writeError(w, http.StatusBadRequest, "the new password must differ from the current one")
		return
	}
	if err := s.cfg.Settings.PasswordPolicy(r.Context()).ValidatePassword(payload.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.Hash(payload.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot hash the new password")
		return
	}
	if err := s.cfg.Store.SetPasswordHash(r.Context(), user.ID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot store the new password")
		return
	}
	s.loginLimit.reset(r, user.Username)
	// Every other session dies with the old password; the caller keeps working.
	if _, err := s.cfg.Store.DeleteUserSessions(r.Context(), user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "password changed but sessions could not be revoked")
		return
	}
	if err := s.startSession(r, w, user, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "password changed, please sign in again")
		return
	}
	passwordLog.Tag("SUCCESS").Info("password rotated for " + user.Username)
	w.WriteHeader(http.StatusNoContent)
}
