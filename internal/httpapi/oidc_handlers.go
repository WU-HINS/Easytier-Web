package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/oidc"
	"github.com/easytier/easytier-console/internal/settings"
)

const oidcStateCookie = "et_oidc_state"
const oidcCallbackPath = "/api/v1/auth/oidc/callback"

// reloadOIDC rebuilds the provider from the settings table. An incomplete or
// disabled configuration simply means "no provider", never a stale one.
func (s *Server) reloadOIDC(ctx context.Context) error {
	cfg := s.cfg.Settings
	if !cfg.Bool(ctx, settings.KeyOIDCEnabled, false) {
		s.setOIDC(nil)
		if s.cfg.OIDC != nil {
			// The administrator has not configured OIDC in the console; the
			// provider built from EASYTIER_OIDC_* stays in use.
			log.Sub("OIDC").Tag("SEED").Info("console settings leave OIDC off; falling back to the environment provider")
		}
		return nil
	}
	issuer := cfg.String(ctx, settings.KeyOIDCIssuer, "")
	clientID := cfg.String(ctx, settings.KeyOIDCClientID, "")
	if issuer == "" || clientID == "" {
		s.setOIDC(nil)
		return nil
	}
	label := cfg.String(ctx, settings.KeyOIDCButtonLabel, "使用 SSO 登录")
	allowInsecure := cfg.Bool(ctx, "oidc.allow_insecure", false)
	if !allowInsecure && !strings.HasPrefix(issuer, "https://") {
		return errors.New("oidc: issuer must use https (enable oidc.allow_insecure for local development)")
	}
	provider, err := oidc.New(ctx, oidc.Config{
		Issuer:        issuer,
		ClientID:      clientID,
		ClientSecret:  cfg.String(ctx, settings.KeyOIDCClientSecret, ""),
		Scopes:        strings.Fields(cfg.String(ctx, settings.KeyOIDCScopes, "openid profile email")),
		ButtonLabel:   label,
		AllowInsecure: allowInsecure,
	})
	if err != nil {
		return err
	}
	s.setOIDC(provider)
	log.Sub("OIDC").Tag("READY").Info("provider configured " + issuer)
	return nil
}

func (s *Server) setOIDC(provider *oidc.Provider) {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	s.oidcProvider = provider
}

func (s *Server) currentOIDC() *oidc.Provider {
	s.oidcMu.RLock()
	defer s.oidcMu.RUnlock()
	if s.oidcProvider != nil {
		return s.oidcProvider
	}
	return s.cfg.OIDC
}

// routesOIDC mounts the OpenID Connect endpoints. The config endpoint always
// exists so the login page and the account page can ask for the state.
func (s *Server) routesOIDC() {
	s.mux.HandleFunc("GET /api/v1/auth/oidc/config", s.handleOIDCConfig)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/login", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/callback", s.handleOIDCCallback)
	s.mux.HandleFunc("POST /api/v1/auth/oidc/register", s.handleOIDCRegister)
}

func (s *Server) handleOIDCConfig(w http.ResponseWriter, r *http.Request) {
	provider := s.currentOIDC()
	if provider == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":      true,
		"button_label": provider.Config().ButtonLabel,
		"login_path":   "/auth/oidc/login",
		"bind_path":    "/account/oidc/bind",
	})
}

// validateCallbackURL enforces the rule that the callback is produced by the
// browser: https only, same host as this request, fixed path.
// requestScheme reports the scheme the browser used. Behind a TLS terminating
// proxy the connection is plain, so X-Forwarded-Proto is honoured only when the
// operator declared the proxy trusted (server.trust_proxy).
func (s *Server) requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s.cfg.Settings != nil && s.cfg.Settings.Bool(r.Context(), settings.KeyTrustProxy, false) {
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			return "https"
		}
	}
	return "http"
}

func (s *Server) validateCallbackURL(raw string, r *http.Request, allowInsecure bool) (string, error) {
	if raw == "" {
		raw = s.requestScheme(r) + "://" + r.Host + oidcCallbackPath
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("callback url is not a valid URL")
	}
	if parsed.Path != oidcCallbackPath {
		return "", fmt.Errorf("callback url must end with %s", oidcCallbackPath)
	}
	if parsed.Host != r.Host {
		return "", fmt.Errorf("callback url must use the current host")
	}
	if parsed.Scheme == "https" {
		return parsed.String(), nil
	}
	if parsed.Scheme == "http" && allowInsecure && isLoopback(r.Host) {
		return parsed.String(), nil
	}
	return "", fmt.Errorf("callback url must use https")
}

func isLoopback(host string) bool {
	hostname := host
	if colon := strings.LastIndex(host, ":"); colon > 0 {
		hostname = host[:colon]
	}
	if hostname == "localhost" {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) allowInsecureOIDC(r *http.Request) bool {
	return s.cfg.Settings.Bool(r.Context(), "oidc.allow_insecure", false)
}

func (s *Server) startOIDCFlow(w http.ResponseWriter, r *http.Request, mode string, userID int64) {
	provider := s.currentOIDC()
	if provider == nil {
		writeError(w, http.StatusNotFound, "oidc is not configured")
		return
	}
	redirect, err := s.validateCallbackURL(r.URL.Query().Get("redirect_url"), r, s.allowInsecureOIDC(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err1 := oidc.RandomString(32)
	nonce, err2 := oidc.RandomString(32)
	verifier, err3 := oidc.RandomString(48)
	if err1 != nil || err2 != nil || err3 != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate login material")
		return
	}
	bag, err := oidc.EncodeState(s.cfg.SessionKey, oidc.StateBag{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Expires:  s.cfg.Now().Add(10 * time.Minute).Unix(),
		Redirect: redirect,
		Mode:     mode,
		UserID:   userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot sign login state")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    bag,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	target, err := provider.WithRedirect(redirect).AuthorizationURL(r.Context(), state, nonce, verifier)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity provider is unavailable")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	s.startOIDCFlow(w, r, "login", 0)
}

func (s *Server) handleAccountOIDCBind(w http.ResponseWriter, r *http.Request, user bal.User) {
	s.startOIDCFlow(w, r, "bind", user.ID)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	provider := s.currentOIDC()
	if provider == nil {
		writeError(w, http.StatusNotFound, "oidc is not configured")
		return
	}
	query := r.URL.Query()
	if failure := query.Get("error"); failure != "" {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("identity provider returned %s: %s", failure, query.Get("error_description")))
		return
	}
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing login state")
		return
	}
	bag, err := oidc.DecodeState(s.cfg.SessionKey, cookie.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "login state is invalid or expired")
		return
	}
	if query.Get("state") != bag.State {
		writeError(w, http.StatusBadRequest, "login state mismatch")
		return
	}
	view := provider.WithRedirect(bag.Redirect)
	claims, idToken, err := view.Exchange(r.Context(), query.Get("code"), bag.Verifier, bag.Nonce)
	if err != nil {
		log.Sub("OIDC").Tag("DENY").Warn("exchange failed: " + err.Error())
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})

	if bag.Mode == "bind" {
		session, err := s.requireSessionUser(r)
		if err != nil || session.ID != bag.UserID {
			writeError(w, http.StatusUnauthorized, "bind session does not match")
			return
		}
		if err := s.cfg.Store.SetUserOIDC(r.Context(), session.ID, claims.Issuer, claims.Subject); err != nil {
			if errors.Is(err, bal.ErrOIDCIdentityTaken) {
				writeError(w, http.StatusConflict, "this identity is already bound to another account")
				return
			}
			log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		log.Sub("Account").Tag("OIDC").Info("oidc bound " + session.Username)
		http.Redirect(w, r, "/?bound=1", http.StatusFound)
		return
	}

	user, err := s.provisionOIDCUser(r, claims)
	if err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user.Disabled {
		writeError(w, http.StatusForbidden, "account is disabled")
		return
	}
	if err := s.startSession(r, w, user, idToken); err != nil {
		log.Sub("HTTP").Tag("ERROR").Error("unhandled: " + err.Error())
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// provisionOIDCUser maps a verified identity to a console account.
func (s *Server) provisionOIDCUser(r *http.Request, claims oidc.Claims) (bal.User, error) {
	if existing, err := s.cfg.Store.GetUserByOIDC(r.Context(), claims.Issuer, claims.Subject); err == nil {
		return existing, nil
	} else if !errors.Is(err, bal.ErrNotFound) {
		return bal.User{}, err
	}
	username := strings.TrimSpace(claims.PreferredUsername)
	if username == "" && claims.Email != "" {
		username = strings.SplitN(claims.Email, "@", 2)[0]
	}
	if username == "" {
		subject := claims.Subject
		if len(subject) > 8 {
			subject = subject[:8]
		}
		username = "oidc-" + subject
	}
	// The store owns the "pick a free name" retry: doing it here would be a
	// read-then-write that two concurrent first time logins can both pass.
	candidate := username
	if err := s.cfg.Settings.ValidateUsername(r.Context(), candidate); err != nil {
		candidate = fmt.Sprintf("oidc-%d", time.Now().UnixNano()%1_000_000)
	}
	return s.cfg.Store.UpsertOIDCUser(r.Context(), claims.Issuer, claims.Subject, candidate, claims.Email, claims.Name)
}

// PasswordLogoutURL ends the provider session too when configured.
func (s *Server) PasswordLogoutURL(r *http.Request) string {
	provider := s.currentOIDC()
	if provider == nil {
		return ""
	}
	postLogout := s.cfg.PostLogoutRedirect
	if postLogout == "" {
		postLogout = s.requestScheme(r) + "://" + r.Host + "/"
	}
	if _, err := url.Parse(postLogout); err != nil {
		return ""
	}
	return provider.LogoutURL("", postLogout)
}
