package httpapi

import (
	"github.com/easytier/easytier-console/internal/settings"
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/wire"
)

// handleUploadPathHint answers a device manager that forgot the username with a
// diagnosable error instead of the single page app.
func handleUploadPathHint(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusBadRequest, "device upload path must be /@up/<username>")
}

// handleDeviceWS mounts the device control channel on the web port:
//
//	ws://<console>/@up/<username>
//
// The username in the path is a routing hint; the authoritative identity is the
// token in the first heartbeat, and both must agree.
func (s *Server) handleDeviceWS(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "" {
		writeError(w, http.StatusBadRequest, "missing username in upload path")
		return
	}
	s.serveDeviceChannel(w, r, username)
}

// handleDeviceWSRoot serves the same channel for clients that predate the /@up
// path: the 2.4.0 and 2.5.0 releases dial ws://host:port/ and carry the token in
// their first heartbeat, so the path cannot name the user. The upgrade is the
// only thing that distinguishes them from a browser asking for the page.
func (s *Server) handleDeviceWSRoot(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		http.NotFound(w, r)
		return
	}
	s.serveDeviceChannel(w, r, "")
}

// isWebSocketUpgrade reports whether the request asks to become a websocket.
func isWebSocketUpgrade(r *http.Request) bool {
	for _, value := range r.Header.Values("Upgrade") {
		if strings.EqualFold(strings.TrimSpace(value), "websocket") {
			return true
		}
	}
	return false
}

func (s *Server) serveDeviceChannel(w http.ResponseWriter, r *http.Request, username string) {
	// Bound concurrent device channels so a flood of half open sockets cannot
	// pin unbounded goroutines.
	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "too many device connections")
		return
	}
	tunnel, err := wire.UpgradeWebSocket(w, r, func() bool {
		// Same switch the HTTP side uses: an untrusted peer must not choose
		// the address we record for it.
		return s.cfg.Settings.Bool(r.Context(), settings.KeyTrustProxy, false)
	})
	if err != nil {
		return
	}
	log.Sub("Upload").Tag("CONNECT").Info("device channel opened for " + username + " from " + tunnel.RemoteAddr())
	if err := s.cfg.Sessions.ServeAs(r.Context(), tunnel, username); err != nil {
		log.Sub("Upload").Tag("CLOSE").Info("device channel closed: " + err.Error())
	}
}
