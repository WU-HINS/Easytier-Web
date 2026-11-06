package httpapi

import (
	"net/http"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// handleRotateNetworkSecret replaces the network secret with a fresh random one.
//
// Rotating is not a rename: every member's compiled config carries the secret, so
// the next reconcile pushes the new one to devices this console manages. Devices
// that joined by hand (a pasted config or a temporary credential) keep the old
// secret and stop being able to reach the network, which is the point of a rotate
// and why the UI asks for confirmation first.
func (s *Server) handleRotateNetworkSecret(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	secret, err := auth.RandomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate a secret")
		return
	}
	network.NetworkSecret = secret
	network.UpdateTime = s.cfg.Now()
	if err := s.cfg.Store.UpdateNetwork(r.Context(), network); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update the network")
		return
	}
	// The secret is returned once so an operator can hand it to a device that is
	// not managed by this console; it is never part of the network listing.
	writeJSON(w, http.StatusOK, map[string]any{
		"network_secret": secret,
		"rotated_at":     network.UpdateTime.UTC().Format(time.RFC3339),
	})
}
