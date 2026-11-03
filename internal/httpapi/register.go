package httpapi

import (
	"errors"
	"github.com/easytier/easytier-console/internal/db/bal"
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/settings"
)

// handleRegister creates a normal user account. It honours the registration
// switch, the reserved username list and the configured password policy; the
// instance quota is enforced when instances are registered, not here.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Settings.Bool(r.Context(), settings.KeyRegistrationOpen, true) {
		writeError(w, http.StatusForbidden, "registration is closed")
		return
	}
	var payload struct {
		Username string "json:\"username\""
		Password string "json:\"password\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	username := strings.TrimSpace(payload.Username)
	if err := s.cfg.Settings.ValidateUsername(r.Context(), username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	taken, err := s.cfg.Store.ExistsByUsername(r.Context(), username)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if taken {
		writeError(w, http.StatusConflict, "username is already taken")
		return
	}
	if err := s.cfg.Settings.PasswordPolicy(r.Context()).ValidatePassword(payload.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := auth.Hash(payload.Password)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	user, err := s.cfg.Store.CreateUser(r.Context(), username, hash)
	if err != nil {
		// The pre-check above is only a fast path; the unique index is what makes
		// this correct when two registrations race.
		if errors.Is(err, bal.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username is already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot create the account")
		return
	}
	log.Sub("Auth").Tag("REGISTER").Info("user registered " + user.Username)
	if err := s.startSession(r, w, user, ""); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "role": user.Role})
}
