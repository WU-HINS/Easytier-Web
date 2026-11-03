package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/logging"
	"github.com/easytier/easytier-console/internal/settings"
)

var log = logging.New("HTTP")

const (
	codeSetupRequired      = "setup_required"
	codeAlreadyInitialized = "already_initialized"
	codeQuotaExceeded      = "quota_exceeded"
	codeUserHasInstances   = "user_has_online_instances"
)

// SetupStatus is what the client asks for before rendering anything.
type SetupStatus struct {
	Initialized bool "json:\"initialized\""
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.cfg.Settings.Initialized(r.Context())
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, SetupStatus{Initialized: initialized})
}

// handleSetup creates the single administrator and closes the setup endpoint.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.cfg.Settings.Initialized(r.Context())
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if initialized {
		writeErrorCode(w, http.StatusConflict, codeAlreadyInitialized, "the console is already initialized")
		return
	}
	if !s.setupTokenMatches(r) {
		log.Sub("Setup").Tag("DENY").Warn("setup rejected: missing or wrong setup token")
		writeError(w, http.StatusUnauthorized, "a setup token is required; send it in the X-Setup-Token header")
		return
	}
	var payload struct {
		Username string "json:\"username\""
		Password string "json:\"password\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	if err := s.cfg.Settings.ValidateUsername(r.Context(), payload.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	admin, err := s.cfg.Store.CreateAdmin(r.Context(), strings.TrimSpace(payload.Username), hash)
	if err != nil {
		if errors.Is(err, bal.ErrAdminExists) {
			writeErrorCode(w, http.StatusConflict, codeAlreadyInitialized, "the console is already initialized")
			return
		}
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.cfg.Settings.MarkInitialized(r.Context()); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Sub("Setup").Tag("SUCCESS").Info("administrator created " + admin.Username)
	if err := s.startSession(r, w, admin, ""); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": admin.Username, "role": admin.Role})
}

// requireAdmin is the single choke point for the admin surface.
func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request, bal.User)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, user bal.User) {
		if user.Role != bal.RoleAdmin {
			log.Sub("Authz").Tag("DENY").Warn("non-admin called " + r.URL.Path)
			writeError(w, http.StatusForbidden, "administrator role required")
			return
		}
		next(w, r, user)
	})
}

type adminUserItem struct {
	ID              int64  "json:\"id\""
	Username        string "json:\"username\""
	Role            string "json:\"role\""
	Disabled        bool   "json:\"disabled\""
	OIDCBound       bool   "json:\"oidc_bound\""
	CreatedAt       string "json:\"created_at\""
	DeviceCount     int    "json:\"device_count\""
	OnlineInstances int    "json:\"online_instances\""
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request, _ bal.User) {
	users, err := s.cfg.Store.ListUsers(r.Context())
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]adminUserItem, 0, len(users))
	for _, user := range users {
		devices, err := s.cfg.Store.ListDevices(r.Context(), user.ID)
		if err != nil {
			log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		items = append(items, adminUserItem{
			ID:              user.ID,
			Username:        user.Username,
			Role:            user.Role,
			Disabled:        user.Disabled,
			OIDCBound:       user.OIDCSubject != "",
			CreatedAt:       user.CreatedAt.Format(time.RFC3339),
			DeviceCount:     len(devices),
			OnlineInstances: s.onlineInstances(user.ID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": items})
}

// onlineInstances counts instances reported by live sessions of one user.
func (s *Server) onlineInstances(userID int64) int {
	total := 0
	for _, snapshot := range s.cfg.Sessions.Snapshots(userID) {
		if snapshot.Online {
			total += len(snapshot.Instances)
		}
	}
	return total
}

func (s *Server) handleAdminSetDisabled(w http.ResponseWriter, r *http.Request, admin bal.User) {
	id, err := parseID(r.PathValue("userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var payload struct {
		Disabled bool "json:\"disabled\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	target, err := s.cfg.Store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if target.Role == bal.RoleAdmin {
		writeError(w, http.StatusForbidden, "the administrator account cannot be disabled")
		return
	}
	if err := s.cfg.Store.SetUserDisabled(r.Context(), id, payload.Disabled); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if payload.Disabled {
		// Disabling must take effect now: drop the open console sessions and any
		// live device tunnel. Otherwise the account keeps working until the
		// session cookie expires (24h by default).
		if _, err := s.cfg.Store.DeleteUserSessions(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot revoke user sessions")
			return
		}
		if s.cfg.Sessions != nil {
			s.cfg.Sessions.DisconnectUser(id)
		}
	}
	log.Sub("Admin").Tag("USER").Info(fmt.Sprintf("disabled=%t for %s by %s", payload.Disabled, target.Username, admin.Username))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request, admin bal.User) {
	id, err := parseID(r.PathValue("userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	target, err := s.cfg.Store.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if target.Role == bal.RoleAdmin {
		writeError(w, http.StatusForbidden, "the administrator account cannot be deleted")
		return
	}
	if online := s.onlineInstances(id); online > 0 {
		writeErrorCode(w, http.StatusConflict, codeUserHasInstances,
			fmt.Sprintf("user has %d online instance(s); stop them before deleting", online))
		return
	}
	if err := s.cfg.Store.DeleteUser(r.Context(), id); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Close the check-then-delete window: anything that reconnected between the
	// quota check and the delete is torn down now and logged.
	if stopped := s.cfg.Sessions.DisconnectUser(id); stopped > 0 {
		log.Sub("Admin").Tag("RACE").Warn(fmt.Sprintf("disconnected %d session(s) of deleted user %s", stopped, target.Username))
	}
	log.Sub("Admin").Tag("USER").Info("user deleted " + target.Username + " by " + admin.Username)
	w.WriteHeader(http.StatusNoContent)
}

// settingsView is the redacted, client facing settings payload.
type settingsView struct {
	MaxInstancesPerUser int    "json:\"max_instances_per_user\""
	PasswordMinLength   int    "json:\"password_min_length\""
	PasswordMinClasses  int    "json:\"password_min_classes\""
	ReservedUsernames   string "json:\"reserved_usernames\""
	BackgroundURL       string "json:\"background_url\""
	RegistrationOpen    bool   "json:\"registration_open\""
	OIDCEnabled         bool   "json:\"oidc_enabled\""
	OIDCIssuer          string "json:\"oidc_issuer\""
	OIDCClientID        string "json:\"oidc_client_id\""
	OIDCClientSecretSet bool   "json:\"oidc_client_secret_set\""
	OIDCButtonLabel     string "json:\"oidc_button_label\""
	ExtraTCPEnabled     bool   "json:\"extra_tcp_enabled\""
	ExtraUDPEnabled     bool   "json:\"extra_udp_enabled\""
	ExtraPort           int    "json:\"extra_port\""
	Revision            int    "json:\"revision\""
}

func (s *Server) readSettings(ctx context.Context) settingsView {
	cfg := s.cfg.Settings
	revision, err := s.cfg.Store.SettingsRevision(ctx)
	if err != nil {
		revision = 0
	}
	return settingsView{
		MaxInstancesPerUser: cfg.MaxInstancesPerUser(ctx),
		PasswordMinLength:   cfg.Int(ctx, settings.KeyPasswordMinLength, 10),
		PasswordMinClasses:  cfg.Int(ctx, settings.KeyPasswordClasses, 3),
		ReservedUsernames:   cfg.String(ctx, settings.KeyReservedNames, settings.DefaultReserved),
		BackgroundURL:       cfg.String(ctx, settings.KeyBackgroundURL, ""),
		RegistrationOpen:    cfg.Bool(ctx, settings.KeyRegistrationOpen, true),
		OIDCEnabled:         cfg.Bool(ctx, settings.KeyOIDCEnabled, false),
		OIDCIssuer:          cfg.String(ctx, settings.KeyOIDCIssuer, ""),
		OIDCClientID:        cfg.String(ctx, settings.KeyOIDCClientID, ""),
		OIDCClientSecretSet: cfg.String(ctx, settings.KeyOIDCClientSecret, "") != "",
		OIDCButtonLabel:     cfg.String(ctx, settings.KeyOIDCButtonLabel, "使用 SSO 登录"),
		ExtraTCPEnabled:     cfg.Bool(ctx, settings.KeyExtraTCPEnabled, false),
		ExtraUDPEnabled:     cfg.Bool(ctx, settings.KeyExtraUDPEnabled, false),
		ExtraPort:           cfg.Int(ctx, settings.KeyExtraPort, 0),
		Revision:            revision,
	}
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request, _ bal.User) {
	writeJSON(w, http.StatusOK, s.readSettings(r.Context()))
}

func (s *Server) handleAdminPutSettings(w http.ResponseWriter, r *http.Request, admin bal.User) {
	var payload map[string]any
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	ctx := r.Context()
	if err := validateSettingPatch(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Build the patch first, validate the resulting state, then write it in one
	// transaction. A rejected request must not leave a half applied config.
	var expected *int
	if raw, ok := payload["expected_revision"]; ok {
		value := toInt(raw)
		if value < 0 {
			writeError(w, http.StatusBadRequest, "expected_revision must not be negative")
			return
		}
		expected = &value
	}
	updates := map[string]string{}
	for field, raw := range payload {
		switch field {
		case "max_instances_per_user":
			updates[settings.KeyMaxInstances] = fmt.Sprintf("%d", toInt(raw))
		case "password_min_length":
			updates[settings.KeyPasswordMinLength] = fmt.Sprintf("%d", toInt(raw))
		case "password_min_classes":
			updates[settings.KeyPasswordClasses] = fmt.Sprintf("%d", toInt(raw))
		case "reserved_usernames":
			updates[settings.KeyReservedNames] = toString(raw)
		case "background_url":
			candidate := toString(raw)
			if candidate != "" {
				parsed, err := url.Parse(candidate)
				if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					writeError(w, http.StatusBadRequest, "background_url must be an http(s) URL")
					return
				}
			}
			updates[settings.KeyBackgroundURL] = candidate
		case "registration_open":
			updates[settings.KeyRegistrationOpen] = fmt.Sprintf("%t", toBool(raw))
		case "oidc_enabled":
			updates[settings.KeyOIDCEnabled] = fmt.Sprintf("%t", toBool(raw))
		case "oidc_issuer":
			updates[settings.KeyOIDCIssuer] = strings.TrimSuffix(toString(raw), "/")
		case "oidc_client_id":
			updates[settings.KeyOIDCClientID] = toString(raw)
		case "oidc_client_secret":
			updates[settings.KeyOIDCClientSecret] = toString(raw)
		case "oidc_button_label":
			updates[settings.KeyOIDCButtonLabel] = toString(raw)
		case "extra_tcp_enabled":
			updates[settings.KeyExtraTCPEnabled] = fmt.Sprintf("%t", toBool(raw))
		case "extra_udp_enabled":
			updates[settings.KeyExtraUDPEnabled] = fmt.Sprintf("%t", toBool(raw))
		case "extra_port":
			updates[settings.KeyExtraPort] = fmt.Sprintf("%d", toInt(raw))
		}
	}

	// Prospective state = current values overlaid with the patch.
	prospective := func(key, fallback string) string {
		if value, ok := updates[key]; ok {
			return value
		}
		return s.cfg.Settings.String(ctx, key, fallback)
	}
	oidcEnabled := prospective(settings.KeyOIDCEnabled, "false") == "true"
	if err := validateSettingsState(
		oidcEnabled,
		prospective(settings.KeyOIDCIssuer, ""),
		prospective(settings.KeyOIDCClientID, ""),
		s.cfg.Settings.Bool(ctx, "oidc.allow_insecure", false),
	); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	revision, err := s.cfg.Store.PutSettings(ctx, updates, expected)
	if err != nil {
		var conflict *bal.ErrSettingsConflict
		if errors.As(err, &conflict) {
			log.Sub("Admin").Tag("CONFLICT").Warn("settings conflict for " + admin.Username)
			writeJSON(w, http.StatusConflict, map[string]any{
				"message":  "settings were changed by another session",
				"code":     "settings_conflict",
				"revision": conflict.Current,
			})
			return
		}
		// Never echo the store error: it carries table and constraint names.
		log.Sub("Admin").Tag("ERROR").Error("settings write failed: " + err.Error())
		writeError(w, http.StatusInternalServerError, "cannot save settings")
		return
	}
	_ = revision
	log.Sub("Admin").Tag("SETTINGS").Info(fmt.Sprintf("%s updated %d setting(s)", admin.Username, len(updates)))
	if err := s.reloadOIDC(ctx); err != nil {
		log.Sub("OIDC").Tag("ERROR").Error("oidc reload failed: " + err.Error())
		writeError(w, http.StatusBadGateway, "settings saved but OIDC reload failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.readSettings(ctx))
}

// AccountOIDCStatus is the self-service binding state.
type AccountOIDCStatus struct {
	Bound  bool   "json:\"bound\""
	Issuer string "json:\"issuer\""
}

func (s *Server) handleAccountOIDCStatus(w http.ResponseWriter, r *http.Request, user bal.User) {
	writeJSON(w, http.StatusOK, AccountOIDCStatus{Bound: user.OIDCSubject != "", Issuer: user.OIDCIssuer})
}

func (s *Server) handleAccountOIDCUnbind(w http.ResponseWriter, r *http.Request, user bal.User) {
	if err := s.cfg.Store.ClearUserOIDC(r.Context(), user.ID); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	log.Sub("Account").Tag("OIDC").Info("oidc unbound " + user.Username)
	w.WriteHeader(http.StatusNoContent)
}

func toInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		var parsed int
		_, _ = fmt.Sscanf(typed, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func toBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	default:
		return false
	}
}

func toString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprintf("%v", value)
}

func parseID(raw string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(raw, "%d", &id)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// setupTokenMatches enforces the optional setup token. An empty configured
// token keeps the historical behaviour (first request wins).
func (s *Server) setupTokenMatches(r *http.Request) bool {
	expected := s.cfg.SetupToken
	if expected == "" {
		return true
	}
	given := r.Header.Get("X-Setup-Token")
	return subtle.ConstantTimeCompare([]byte(given), []byte(expected)) == 1
}
