package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// A first time OIDC login has no account yet and must not get one by default:
// the operator picks the username, and that name never changes afterwards. The
// provider round trip and the username form are two requests, so the verified
// identity travels between them in this short lived signed cookie: the client
// may hold it, but not edit it.
const (
	pendingRegistrationCookie = "et_oidc_pending"
	pendingRegistrationTTL    = 10 * time.Minute
)

var errNoPendingRegistration = errors.New("httpapi: no pending oidc registration")

type pendingRegistration struct {
	Issuer  string `json:"iss"`
	Subject string `json:"sub"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Expiry  int64  `json:"exp"`
}

func signPending(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

func encodePendingRegistration(key []byte, pending pendingRegistration, now time.Time) string {
	pending.Expiry = now.Add(pendingRegistrationTTL).Unix()
	payload, err := json.Marshal(pending)
	if err != nil {
		return ""
	}
	mac := signPending(key, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

func decodePendingRegistration(key []byte, value string, now time.Time) (pendingRegistration, error) {
	var pending pendingRegistration
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return pending, errNoPendingRegistration
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return pending, errNoPendingRegistration
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return pending, errNoPendingRegistration
	}
	if !hmac.Equal(mac, signPending(key, payload)) {
		return pending, errNoPendingRegistration
	}
	if err := json.Unmarshal(payload, &pending); err != nil {
		return pending, errNoPendingRegistration
	}
	if pending.Issuer == "" || pending.Subject == "" {
		return pending, errNoPendingRegistration
	}
	if now.Unix() >= pending.Expiry {
		return pending, errNoPendingRegistration
	}
	return pending, nil
}

func writePendingRegistration(w http.ResponseWriter, secure bool, value string) {
	http.SetCookie(w, &http.Cookie{Name: pendingRegistrationCookie, Value: value, Path: "/",
		MaxAge: int(pendingRegistrationTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func clearPendingRegistration(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: pendingRegistrationCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// handleOIDCRegister creates the account for a verified but unbound identity.
// The username is chosen here and is never editable afterwards.
func (s *Server) handleOIDCRegister(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(pendingRegistrationCookie)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no pending registration; start the sign-in again")
		return
	}
	pending, err := decodePendingRegistration(s.cfg.SessionKey, cookie.Value, s.cfg.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "no pending registration; start the sign-in again")
		return
	}
	var payload struct {
		Username string
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid registration payload")
		return
	}
	username := strings.TrimSpace(payload.Username)
	if err := s.cfg.Settings.ValidateUsername(r.Context(), username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := s.cfg.Store.UpsertOIDCUser(r.Context(), pending.Issuer, pending.Subject, username, pending.Email, pending.Name)
	if err != nil {
		writeError(w, http.StatusConflict, "that username is not available")
		return
	}
	clearPendingRegistration(w, s.cfg.SecureCookies)
	if err := s.startSession(r, w, user, ""); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Sub("Auth").Tag("OIDC").Info("registered " + user.Username)
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username})
}
