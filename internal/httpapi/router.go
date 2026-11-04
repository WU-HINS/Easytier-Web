// Package httpapi is the console REST surface. It deliberately mirrors the
// 2.7.0 route table so an existing frontend keeps working, while every
// handler is now backed by the rewritten protocol layer.
package httpapi

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/pb/methodindex"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
)

// Config is the wiring of the HTTP surface.
type Config struct {
	Store         *bal.Store
	Sessions      *session.Manager
	SecureCookies bool
	// WebDir, when set, serves the built single page client from disk and
	// falls back to index.html for client side routes.
	WebDir string
	// OIDC, when non-nil, enables the authorization-code login flow.
	OIDC *oidc.Provider
	// SessionKey signs short lived cookies such as the OIDC login state.
	SessionKey []byte
	// PostLogoutRedirect is handed to the provider on logout.
	PostLogoutRedirect string
	// Settings is the admin editable configuration layer.
	Settings *settings.Settings
	// MaxUploadConns bounds concurrent /@up device channels (default 1024).
	MaxUploadConns int
	SessionTTL     time.Duration
	// SetupToken, when set, is required by POST /api/v1/setup. Without it the
	// first request wins, which is fine on localhost and dangerous on a public
	// address.
	SetupToken          string
	DisableRegistration bool
	Now                 func() time.Time
}

const sessionCookie = "et_session"

// Server serves the console REST API.
type Server struct {
	cfg Config
	mux *http.ServeMux

	oidcMu       sync.RWMutex
	oidcProvider *oidc.Provider

	loginLimit  *loginLimiter
	uploadSlots chan struct{}
}

// New builds the HTTP server and its route table.
func New(cfg Config) (*Server, error) {
	if cfg.Store == nil || cfg.Sessions == nil {
		return nil, errors.New("httpapi: store and session manager are required")
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if len(cfg.SessionKey) == 0 {
		key, err := auth.RandomToken(32)
		if err != nil {
			return nil, err
		}
		cfg.SessionKey = []byte(key)
	}
	if cfg.Settings == nil {
		cfg.Settings = settings.New(cfg.Store)
	}
	s := &Server{
		cfg:         cfg,
		mux:         http.NewServeMux(),
		uploadSlots: make(chan struct{}, uploadLimit(cfg.MaxUploadConns)),
		loginLimit: newLoginLimiter(10, 5*time.Minute, func(req *http.Request) bool {
			return cfg.Settings.Bool(req.Context(), settings.KeyTrustProxy, false)
		}),
	}
	s.routes()
	s.routesOIDC()
	if err := s.reloadOIDC(context.Background()); err != nil {
		log.Sub("OIDC").Tag("WARN").Warn("oidc not active: " + err.Error())
	}
	if cfg.WebDir != "" {
		s.mountClient()
	} else {
		// Without a client to serve, the root still belongs to devices that dial it:
		// the 2.4.0 and 2.5.0 releases connect to ws://host:port/ with no path.
		s.mux.HandleFunc("GET /", s.handleDeviceWSRoot)
	}
	return s, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "sessions": s.cfg.Sessions.Count()})
	})
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	// Aliases the original console exposes; see docs/FEATURE-MATRIX.md.
	s.mux.HandleFunc("GET /api/v1/auth/logout", s.handleLogoutCompat)
	s.mux.HandleFunc("GET /api/v1/auth/check_login_status", s.handleCheckLoginStatus)
	s.mux.HandleFunc("GET /api/v1/auth/captcha", s.handleCaptcha)
	s.mux.HandleFunc("POST /api/v1/generate-config", s.requireUser(s.handleGenerateConfigLite))
	s.mux.HandleFunc("POST /api/v1/parse-config", s.requireUser(s.handleParseConfigLite))
	// The original device-first view, translated by internal/legacyview.
	s.mux.HandleFunc("GET /api/v1/machines/{machineID}/networks/info", s.requireUser(s.handleLegacyCollectInfo))
	s.mux.HandleFunc("GET /api/v1/machines/{machineID}/networks/info/{instanceID}", s.requireUser(s.handleLegacyCollectInfo))
	s.mux.HandleFunc("POST /api/v1/machines/{machineID}/networks/metas", s.requireUser(s.handleLegacyMetas))
	s.mux.HandleFunc("GET /api/v1/machines/{machineID}/networks/config/{instanceID}", s.requireUser(s.handleLegacyGetConfig))
	s.mux.HandleFunc("DELETE /api/v1/machines/{machineID}/networks/{instanceID}", s.requireUser(s.handleLegacyDeleteInstance))
	s.mux.HandleFunc("PUT /api/v1/auth/password", s.requireUser(s.handleChangePasswordCompat))
	s.mux.HandleFunc("GET /api/v1/blocked-devices", s.requireUser(s.handleListBlockedDevicesCompat))
	s.mux.HandleFunc("DELETE /api/v1/blocked-devices/{machineID}", s.requireUser(s.handleUnblockDeviceCompat))
	s.mux.HandleFunc("GET /api/v1/console-info", s.requireUser(s.handleConsoleInfo))
	s.mux.HandleFunc("GET /api/v1/summary", s.requireUser(s.handleSummary))
	s.mux.HandleFunc("GET /api/v1/machines", s.requireUser(s.handleListMachines))
	s.mux.HandleFunc("DELETE /api/v1/machines/{machineID}", s.requireUser(s.handleDeleteMachine))
	s.mux.HandleFunc("GET /api/v1/machines/blocked", s.requireUser(s.handleListBlockedDevices))
	s.mux.HandleFunc("DELETE /api/v1/machines/blocked/{machineID}", s.requireUser(s.handleUnblockDevice))
	s.mux.HandleFunc("PUT /api/v1/machines/{machineID}/alias", s.requireUser(s.handleAlias))
	s.mux.HandleFunc("POST /api/v1/machines/{machineID}/proxy-rpc", s.requireUser(s.handleProxyRPC))
	s.mux.HandleFunc("GET /api/v1/rpc-capabilities", s.requireUser(s.handleRPCCapabilities))
	s.mux.HandleFunc("GET /api/v1/networks", s.requireUser(s.handleListNetworks))
	s.mux.HandleFunc("POST /api/v1/networks", s.requireUser(s.handleCreateNetwork))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}", s.requireUser(s.handleGetNetwork))
	s.mux.HandleFunc("DELETE /api/v1/networks/{networkID}", s.requireUser(s.handleDeleteNetwork))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}/members", s.requireUser(s.handleListMembers))
	s.mux.HandleFunc("GET /@up/{username}", s.handleDeviceWS)
	// Explicit handlers so the hint is deployment independent: without them an
	// installation that also serves the SPA would answer with index.html 200.
	s.mux.HandleFunc("GET /@up", handleUploadPathHint)
	s.mux.HandleFunc("GET /@up/", handleUploadPathHint)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	s.mux.HandleFunc("GET /api/v1/setup", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	s.mux.HandleFunc("GET /api/v1/admin/users", s.requireAdmin(s.handleAdminListUsers))
	s.mux.HandleFunc("POST /api/v1/admin/users/{userID}/disabled", s.requireAdmin(s.handleAdminSetDisabled))
	s.mux.HandleFunc("DELETE /api/v1/admin/users/{userID}", s.requireAdmin(s.handleAdminDeleteUser))
	s.mux.HandleFunc("GET /api/v1/admin/settings", s.requireAdmin(s.handleAdminGetSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/settings", s.requireAdmin(s.handleAdminPutSettings))
	s.mux.HandleFunc("GET /api/v1/account/oidc", s.requireUser(s.handleAccountOIDCStatus))
	s.mux.HandleFunc("GET /api/v1/account/oidc/bind", s.requireUser(s.handleAccountOIDCBind))
	s.mux.HandleFunc("POST /api/v1/account/oidc/unbind", s.requireUser(s.handleAccountOIDCUnbind))
	s.mux.HandleFunc("POST /api/v1/account/password", s.requireUser(s.handleChangePassword))
	s.mux.HandleFunc("POST /api/v1/networks/{networkID}/members", s.requireUser(s.handleAddMember))
	s.mux.HandleFunc("PATCH /api/v1/networks/{networkID}", s.requireUser(s.handleUpdateNetwork))
	s.mux.HandleFunc("PATCH /api/v1/networks/{networkID}/members/{deviceID}", s.requireUser(s.handleUpdateMember))
	s.mux.HandleFunc("DELETE /api/v1/networks/{networkID}/members/{deviceID}", s.requireUser(s.handleRemoveMember))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}/acl-policy", s.requireUser(s.handleGetACLPolicy))
	s.mux.HandleFunc("PUT /api/v1/networks/{networkID}/acl-policy", s.requireUser(s.handlePutACLPolicy))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}/advanced", s.requireUser(s.handleGetNetworkAdvanced))
	s.mux.HandleFunc("PUT /api/v1/networks/{networkID}/advanced", s.requireUser(s.handlePutNetworkAdvanced))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}/members/{deviceID}/config", s.requireUser(s.handleGetMemberConfig))
	s.mux.HandleFunc("PUT /api/v1/networks/{networkID}/members/{deviceID}/config", s.requireUser(s.handlePutMemberConfig))
	s.mux.HandleFunc("GET /api/v1/networks/{networkID}/credentials", s.requireUser(s.handleListCredentials))
	s.mux.HandleFunc("POST /api/v1/networks/{networkID}/credentials", s.requireUser(s.handleCreateCredential))
	s.mux.HandleFunc("DELETE /api/v1/networks/{networkID}/credentials/{credentialID}", s.requireUser(s.handleRevokeCredential))
}

// Handler returns the root handler with cookie parsing and CSRF guarding.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.cfg.SecureCookies, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isInitialized(r.Context()) && requiresSetup(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeErrorCode(w, http.StatusConflict, codeSetupRequired,
					"console setup has not been completed")
				return
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" && isMutating(r.Method) {
			if !sameOrigin(origin, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin write rejected")
				return
			}
		}
		s.mux.ServeHTTP(w, r)
	}))
}

// mountClient serves the built client. Unknown paths fall back to index.html so
// the client router owns deep links.
func (s *Server) mountClient() {
	dir := s.cfg.WebDir
	files := http.FileServer(http.Dir(dir))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) && (r.URL.Path == "/" || r.URL.Path == "/ws") {
			s.handleDeviceWSRoot(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			// The SPA fallback must never answer API calls.
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if r.URL.Path == "/@up" || strings.HasPrefix(r.URL.Path, "/@up/") {
			// A device manager pointed at the wrong URL must get a diagnosable
			// answer, not a 200 with the single page app.
			writeError(w, http.StatusBadRequest, "device upload path must be /@up/<username>")
			return
		}
		if r.URL.Path != "/" {
			if _, err := os.Stat(filepath.Join(dir, filepath.Clean(r.URL.Path))); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// uploadLimit applies the default bound on concurrent device channels.
func uploadLimit(configured int) int {
	if configured > 0 {
		return configured
	}
	return 1024
}

// isInitialized caches nothing on purpose: setup flips this once, and the cost
// of the check is one indexed row read.
func (s *Server) isInitialized(ctx context.Context) bool {
	initialized, err := s.cfg.Settings.Initialized(ctx)
	if err != nil {
		log.Sub("Setup").Tag("ERROR").Error("initialized check failed: " + err.Error())
		return false
	}
	return initialized
}

// requiresSetup reports whether a request must wait for the setup flow.
func requiresSetup(r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz", "/api/v1/setup":
		return false
	}
	// Static assets stay reachable so the setup page can render.
	return strings.HasPrefix(r.URL.Path, "/api/")
}

// requireSessionUser returns the caller of a request that already passed
// requireUser (used by the OIDC bind callback, which is a browser redirect).
func (s *Server) requireSessionUser(r *http.Request) (bal.User, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return bal.User{}, err
	}
	session, err := s.cfg.Store.GetSession(r.Context(), cookie.Value)
	if err != nil {
		return bal.User{}, err
	}
	return s.cfg.Store.GetUserByID(r.Context(), session.UserID)
}

func isMutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func sameOrigin(origin, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == host
}

type ctxKey int

const userKey ctxKey = 1

// requireUser resolves the session cookie before delegating.
func (s *Server) requireUser(next func(http.ResponseWriter, *http.Request, bal.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		sess, err := s.cfg.Store.GetSession(r.Context(), cookie.Value)
		if err != nil {
			// Why a session is refused matters when an operator debugs a logout
			// loop; the response stays deliberately vague.
			log.Sub("Auth").Tag("DENY").Debug("session rejected: " + err.Error())
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		user, err := s.cfg.Store.GetUserByID(r.Context(), sess.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unknown user")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)), user)
	}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var creds credentials
	if err := decodeJSON(w, r, &creds); err != nil {
		return
	}
	if allowed, retryAfter := s.loginLimit.allow(r, creds.Username); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		log.Sub("Auth").Tag("THROTTLE").Warn("login throttled for " + creds.Username)
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	user, err := s.cfg.Store.GetUserByName(r.Context(), strings.TrimSpace(creds.Username))
	if err != nil {
		// Count unknown accounts against the same buckets, and spend the same
		// argon2 work, so the response time does not reveal whether the user
		// exists.
		s.loginLimit.fail(r, creds.Username)
		_ = auth.Verify(creds.Password, dummyPasswordHash())
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := verifyPassword(creds.Password, user.PasswordHash); err != nil {
		s.loginLimit.fail(r, creds.Username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if user.Disabled {
		log.Sub("Auth").Tag("DENY").Warn("disabled account login " + user.Username)
		writeError(w, http.StatusForbidden, "account is disabled")
		return
	}
	s.loginLimit.reset(r, creds.Username)
	if err := s.startSession(r, w, user, ""); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username": user.Username, "display_name": user.DisplayName, "role": user.Role,
	})
}

// dummyPasswordHash is a fixed PHC string used to burn the same CPU when the
// account does not exist (timing equalisation for login).
var (
	dummyOnce sync.Once
	dummyHash string
)

func dummyPasswordHash() string {
	dummyOnce.Do(func() {
		hash, err := auth.Hash("easytier-console-timing-equaliser")
		if err == nil {
			dummyHash = hash
		}
	})
	return dummyHash
}

// verifyPassword accepts the raw password and, for databases migrated from the
// 2.7.0 frontend, the MD5 pre-hash that client sends.
func verifyPassword(candidate, stored string) error {
	if err := auth.Verify(candidate, stored); err == nil {
		return nil
	}
	sum := md5.Sum([]byte(candidate))
	if err := auth.Verify(hex.EncodeToString(sum[:]), stored); err == nil {
		return nil
	}
	return auth.ErrMismatch
}

// startSession mints a console session and sets the cookie. idTokenHint is kept
// so an OIDC logout can end the provider session too.
func (s *Server) startSession(r *http.Request, w http.ResponseWriter, user bal.User, idTokenHint string) error {
	token, err := auth.RandomToken(32)
	if err != nil {
		return err
	}
	now := s.cfg.Now()
	if err := s.cfg.Store.PutSession(r.Context(), bal.SessionData{
		SID: token, UserID: user.ID, Data: idTokenHint,
		Expiry: now.Add(s.cfg.SessionTTL), Created: now,
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(s.cfg.SessionTTL),
	})
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = s.cfg.Store.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	if target := s.PasswordLogoutURL(r); target != "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "logged out", "redirect": target})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "logged out"})
}

func (s *Server) handleConsoleInfo(w http.ResponseWriter, r *http.Request, user bal.User) {
	writeJSON(w, http.StatusOK, map[string]any{
		"username":               user.Username,
		"role":                   user.Role,
		"config_server_protocol": "tcp",
		"webhook_auth":           false,
	})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request, user bal.User) {
	devices, err := s.cfg.Store.ListDevices(r.Context(), user.ID)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	online := 0
	for _, snapshot := range s.cfg.Sessions.Snapshots(user.ID) {
		if snapshot.Online {
			online++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_count":        len(devices),
		"online_device_count": online,
	})
}

type machineItem struct {
	MachineID       string   `json:"machine_id"`
	Hostname        string   `json:"hostname"`
	Alias           string   `json:"alias"`
	EasytierVersion string   `json:"easytier_version"`
	ClientURL       string   `json:"client_url"`
	LastSeen        string   `json:"last_seen"`
	Online          bool     `json:"online"`
	Instances       []string `json:"running_network_instances"`
}

func (s *Server) handleListMachines(w http.ResponseWriter, r *http.Request, user bal.User) {
	devices, err := s.cfg.Store.ListDevices(r.Context(), user.ID)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	live := map[string]session.Snapshot{}
	for _, snapshot := range s.cfg.Sessions.Snapshots(user.ID) {
		live[snapshot.MachineID] = snapshot
	}
	items := make([]machineItem, 0, len(devices))
	for _, device := range devices {
		item := machineItem{
			MachineID:       device.MachineID,
			Hostname:        device.Hostname,
			Alias:           device.Alias,
			EasytierVersion: device.EasytierVersion,
			ClientURL:       device.ClientURL,
			LastSeen:        device.LastSeen.Format(time.RFC3339),
			Instances:       []string{},
		}
		if snapshot, ok := live[device.MachineID]; ok {
			item.Online = snapshot.Online
			if len(snapshot.Instances) > 0 {
				item.Instances = snapshot.Instances
			}
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": items})
}

func (s *Server) handleDeleteMachine(w http.ResponseWriter, r *http.Request, user bal.User) {
	machineID := r.PathValue("machineID")
	block := r.URL.Query().Get("block") == "true"
	if block {
		// Verify ownership before writing: a rejected request must not leave a
		// blocked_devices row behind (review team, r135).
		if _, err := s.cfg.Store.GetDevice(r.Context(), user.ID, machineID); err != nil {
			if errors.Is(err, bal.ErrNotFound) {
				writeError(w, http.StatusNotFound, "device not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "cannot read device")
			return
		}
		if err := s.cfg.Store.BlockDevice(r.Context(), user.ID, machineID, ""); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot block device")
			return
		}
		// Take effect now: an already connected device must lose its tunnel.
		if s.cfg.Sessions != nil {
			s.cfg.Sessions.DisconnectDevice(user.ID, machineID)
		}
	}
	if err := s.cfg.Store.DeleteDevice(r.Context(), user.ID, machineID); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot delete device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAlias(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	if len(payload.Alias) > 64 {
		writeError(w, http.StatusBadRequest, "alias is too long")
		return
	}
	if err := s.cfg.Store.SetDeviceAlias(r.Context(), user.ID, r.PathValue("machineID"), payload.Alias); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// proxyRPCRequest is the same shape the 2.7.0 console posts.
type proxyRPCRequest struct {
	ServiceName string          `json:"service_name"`
	MethodName  string          `json:"method_name"`
	Payload     json.RawMessage `json:"payload"`
}

func (s *Server) handleProxyRPC(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload proxyRPCRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	service, method, ok := resolveMethod(payload.ServiceName, payload.MethodName)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown rpc %s/%s", payload.ServiceName, payload.MethodName))
		return
	}
	// Known in the method index is not the same as usable: the generated Go
	// messages may not include the request or response type.
	if err := methodAvailability(service, payload.MethodName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := newMessage(method.Input)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(payload.Payload) > 0 {
		if err := (protojson.UnmarshalOptions{Resolver: protoregistry.GlobalTypes}).Unmarshal(payload.Payload, in); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
			return
		}
	}
	key, index := splitService(service)
	raw, err := s.cfg.Sessions.Call(r.Context(), user.ID, r.PathValue("machineID"),
		rpc.ServiceKey{ProtoName: key, ServiceName: index}, method.Index, in)
	if err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out, err := newMessage(method.Output)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := proto.Unmarshal(raw, out); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("decode response: %v", err))
		return
	}
	encoded, err := (protojson.MarshalOptions{Resolver: protoregistry.GlobalTypes, EmitUnpopulated: false}).Marshal(out)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

// resolveMethod accepts both the protobuf spelling (ListRoute) and the console
// spelling (list_route).
func lookupMethod(service, method string) (methodindex.Method, bool) {
	methods, ok := methodindex.Services[service]
	if !ok {
		return methodindex.Method{}, false
	}
	if m, ok := methods[method]; ok {
		return m, true
	}
	camel := snakeToCamel(method)
	if m, ok := methods[camel]; ok {
		return m, true
	}
	lower := strings.ToLower(method)
	for name, m := range methods {
		if strings.ToLower(name) == lower {
			return m, true
		}
	}
	return methodindex.Method{}, false
}

func resolveMethod(service, method string) (string, methodindex.Method, bool) {
	m, ok := lookupMethod(service, method)
	if !ok {
		return "", methodindex.Method{}, false
	}
	return service, m, true
}

func snakeToCamel(value string) string {
	var b strings.Builder
	upper := true
	for _, r := range value {
		if r == '_' {
			upper = true
			continue
		}
		if upper {
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// splitService turns "api.manage.WebClientService" into its descriptor pair:
// the leading package becomes proto_name, the trailing symbol service_name.
func splitService(full string) (string, string) {
	index := strings.LastIndex(full, ".")
	if index < 0 {
		return "", full
	}
	return full[:index], full[index+1:]
}

func newMessage(fullName string) (proto.Message, error) {
	desc, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(fullName))
	if err != nil {
		return nil, fmt.Errorf("httpapi: unknown message %s: %w", fullName, err)
	}
	return dynamicpb.NewMessage(desc.Descriptor()), nil
}

func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request, user bal.User) {
	networks, err := s.cfg.Store.ListNetworks(r.Context(), user.ID)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]map[string]any, 0, len(networks))
	for _, network := range networks {
		members, err := s.cfg.Store.ListMembers(r.Context(), user.ID, network.ID)
		if err != nil {
			log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		items = append(items, map[string]any{
			"network_id":        network.ID,
			"display_name":      network.DisplayName,
			"network_name":      network.NetworkName,
			"networking_method": network.NetworkingMethod,
			"secure_mode":       network.SecureMode,
			"member_count":      len(members),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"networks": items})
}

type createNetworkRequest struct {
	Settings struct {
		DisplayName      string   `json:"display_name"`
		NetworkingMethod string   `json:"networking_method"`
		PublicServerURL  *string  `json:"public_server_url"`
		PeerURLs         []string `json:"peer_urls"`
		VirtualCIDR      *string  `json:"virtual_cidr"`
		SecureMode       bool     `json:"secure_mode"`
	} `json:"settings"`
	NetworkSecret *string `json:"network_secret"`
}

func (s *Server) handleCreateNetwork(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload createNetworkRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	settings := payload.Settings
	if strings.TrimSpace(settings.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}
	secret := ""
	if payload.NetworkSecret != nil {
		secret = *payload.NetworkSecret
	}
	if secret == "" {
		generated, err := auth.RandomToken(24)
		if err != nil {
			log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		secret = generated
	}
	now := s.cfg.Now()
	network := bal.Network{
		UserID:           user.ID,
		ID:               mustRandomUUID(),
		DisplayName:      settings.DisplayName,
		NetworkName:      settings.DisplayName,
		NetworkSecret:    secret,
		NetworkingMethod: settings.NetworkingMethod,
		PublicServerURL:  settings.PublicServerURL,
		PeerURLs:         settings.PeerURLs,
		VirtualCIDR:      settings.VirtualCIDR,
		SecureMode:       settings.SecureMode,
		CreateTime:       now,
		UpdateTime:       now,
	}
	if network.NetworkingMethod == "" {
		if len(network.PeerURLs) > 0 {
			network.NetworkingMethod = "Manual"
		} else {
			network.NetworkingMethod = "Standalone"
		}
	}
	if err := validateNetworkIntent(network); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Store.CreateNetwork(r.Context(), network); err != nil {
		// The store owns the constraint translation: a handler must not know what a
		// driver calls a unique violation.
		if errors.Is(err, bal.ErrNetworkExists) {
			writeErrorCode(w, http.StatusConflict, "network_exists", "a network with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot create network")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"network_id":   network.ID,
		"display_name": network.DisplayName,
	})
}

func (s *Server) handleGetNetwork(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"network_id":        network.ID,
		"display_name":      network.DisplayName,
		"network_name":      network.NetworkName,
		"networking_method": network.NetworkingMethod,
		"peer_urls":         network.PeerURLs,
		"virtual_cidr":      network.VirtualCIDR,
		"secure_mode":       network.SecureMode,
	})
}

func (s *Server) handleDeleteNetwork(w http.ResponseWriter, r *http.Request, user bal.User) {
	if err := s.cfg.Store.DeleteNetwork(r.Context(), user.ID, r.PathValue("networkID")); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "network not found")
			return
		}
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request, user bal.User) {
	networkID := r.PathValue("networkID")
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, networkID)
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	members, err := s.cfg.Store.ListMembers(r.Context(), user.ID, networkID)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	plan, compileErr := s.compilePlan(r.Context(), user.ID, network.ID)
	byDevice := map[string]central.MemberPlan{}
	if compileErr == nil {
		for _, member := range plan.Members {
			byDevice[member.DeviceID] = member
		}
	}
	// Running reports what the device actually has; pending reports whether the
	// stored revision still differs from the compiled one. Together they make
	// "the console wants this, did the device get it" visible in the UI.
	running := map[string]bool{}
	for _, instances := range s.cfg.Sessions.OnlineInstances(user.ID) {
		for _, instance := range instances {
			running[instance] = true
		}
	}
	revisions := map[string]string{}
	items := make([]map[string]any, 0, len(members))
	for _, member := range members {
		item := map[string]any{
			"member_id": member.ID,
			"device_id": member.DeviceID,
			"online":    s.isOnline(user.ID, member.DeviceID),
			"temporary": member.CredentialID != nil,
		}
		if compiled, ok := byDevice[member.DeviceID]; ok {
			item["hostname"] = compiled.Hostname
			item["virtual_ipv4"] = compiled.VirtualIPv4
			item["instance_id"] = compiled.InstanceID
			item["running"] = running[compiled.InstanceID]
			raw, ok := revisions[member.DeviceID]
			if !ok {
				if value, err := s.cfg.Store.GetManagedConfigRevision(r.Context(), user.ID, member.DeviceID); err == nil {
					raw = value
				}
				revisions[member.DeviceID] = raw
			}
			item["pending"] = central.DecodeRevisions(raw)[networkID] != central.PlanRevision(networkID, compiled)
		}
		items = append(items, item)
	}
	response := map[string]any{"members": items}
	if compileErr != nil {
		response["compile_error"] = compileErr.Error()
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload struct {
		DeviceIDs []string `json:"device_ids"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	networkID := r.PathValue("networkID")
	if _, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, networkID); err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	quota := s.cfg.Settings.MaxInstancesPerUser(r.Context())
	// The quota is per user, not per network: count every membership the tenant
	// already has, otherwise N networks multiply the allowance by N.
	inUse, err := s.cfg.Store.CountMembersByUser(r.Context(), user.ID)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if quota > 0 && inUse+len(payload.DeviceIDs) > quota {
		writeErrorCode(w, http.StatusConflict, codeQuotaExceeded,
			fmt.Sprintf("instance quota reached: %d in use, limit %d", inUse, quota))
		return
	}
	// Reject duplicates inside one request before touching the database.
	seen := make(map[string]bool, len(payload.DeviceIDs))
	for _, deviceID := range payload.DeviceIDs {
		if seen[deviceID] {
			writeError(w, http.StatusBadRequest, "duplicate device in request: "+deviceID)
			return
		}
		seen[deviceID] = true
	}
	now := s.cfg.Now()
	members := make([]bal.NetworkMember, 0, len(payload.DeviceIDs))
	for _, deviceID := range payload.DeviceIDs {
		if _, err := s.cfg.Store.GetDevice(r.Context(), user.ID, deviceID); err != nil {
			if errors.Is(err, bal.ErrNotFound) {
				writeError(w, http.StatusNotFound, "device not found: "+deviceID)
				return
			}
			writeError(w, http.StatusInternalServerError, "cannot verify device")
			return
		}
		members = append(members, bal.NetworkMember{
			UserID: user.ID, NetworkID: networkID, ID: mustRandomUUID(),
			DeviceID: deviceID, CreateTime: now, UpdateTime: now,
		})
	}
	if err := s.cfg.Store.AddMembersBounded(r.Context(), user.ID, members, quota); err != nil {
		switch {
		case errors.Is(err, bal.ErrMemberExists):
			writeErrorCode(w, http.StatusConflict, "member_exists", "device is already a member of this network")
		case errors.Is(err, bal.ErrDeviceNotFound):
			writeError(w, http.StatusNotFound, "device not found")
		case errors.Is(err, bal.ErrQuotaExceeded):
			// The bounded insert rejects inside the transaction, after the
			// handler level pre-check has already passed. Without this branch two
			// concurrent requests turn a 409 into a 500 (r134).
			writeErrorCode(w, http.StatusConflict, codeQuotaExceeded, "instance quota reached")
		default:
			writeError(w, http.StatusInternalServerError, "cannot add members")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isOnline(userID int64, machineID string) bool {
	for _, snapshot := range s.cfg.Sessions.Snapshots(userID) {
		if snapshot.MachineID == machineID {
			return snapshot.Online
		}
	}
	return false
}

// compilePlan is exported for the reconcile loop; the HTTP layer only uses it to
// preview member addressing.
func (s *Server) compilePlan(ctx context.Context, userID int64, networkID string) (central.Plan, error) {
	network, err := s.cfg.Store.GetNetwork(ctx, userID, networkID)
	if err != nil {
		return central.Plan{}, err
	}
	members, err := s.cfg.Store.ListMembers(ctx, userID, networkID)
	if err != nil {
		return central.Plan{}, err
	}
	devices := map[string]bal.Device{}
	all, err := s.cfg.Store.ListDevices(ctx, userID)
	if err != nil {
		return central.Plan{}, err
	}
	for _, device := range all {
		devices[device.MachineID] = device
	}
	credentials, err := s.cfg.Store.ListCredentials(ctx, userID, networkID)
	if err != nil {
		return central.Plan{}, err
	}
	return central.Compile(network, members, devices, credentials)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return err
	}
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "empty body")
		return errors.New("empty body")
	}
	if err := json.Unmarshal(body, target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"message": message})
}

func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"message": message, "code": code})
}

func mustRandomUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", time.Now().UnixNano()%1e12)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}
