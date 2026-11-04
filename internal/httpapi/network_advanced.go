package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// The network level settings are one JSON blob with the same vocabulary as the
// member override, so the console can store them verbatim and let the compiler
// merge the two layers. The blob is small and bounded, which is why a limit on
// the whitelist matters: reconcile walks it for every device on every pass.
const maxWhitelistEntries = 256

var (
	errWhitelistTooLong = errors.New("whitelist has too many entries")
	errWhitelistEntry   = errors.New("whitelist entries must be non empty and at most 128 characters")
)

// validateNetworkAdvanced checks that the blob parses and that the whitelist is
// within the bounds the reconciler can afford.
func validateNetworkAdvanced(raw *string) error {
	overrides, err := central.ParseOverrides(raw)
	if err != nil {
		return err
	}
	if len(overrides.Whitelist) > maxWhitelistEntries {
		return errWhitelistTooLong
	}
	for _, entry := range overrides.Whitelist {
		if entry == "" || len(entry) > 128 {
			return errWhitelistEntry
		}
	}
	return nil
}

// handleGetNetworkAdvanced returns the stored blob verbatim.
func (s *Server) handleGetNetworkAdvanced(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	advanced := ""
	if network.Advanced != nil {
		advanced = *network.Advanced
	}
	writeJSON(w, http.StatusOK, map[string]any{"advanced": advanced})
}

// handlePutNetworkAdvanced replaces the blob after validating it.
func (s *Server) handlePutNetworkAdvanced(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	var payload struct {
		Advanced string `json:"advanced"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	raw := strings.TrimSpace(payload.Advanced)
	var pointer *string
	if raw != "" {
		if err := validateNetworkAdvanced(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "advanced does not parse: "+err.Error())
			return
		}
		pointer = &raw
	}
	network.Advanced = pointer
	network.UpdateTime = s.cfg.Now()
	if err := s.cfg.Store.UpdateNetwork(r.Context(), network); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update advanced settings")
		return
	}
	log.Sub("Network").Tag("ADVANCED").Info("advanced settings updated " + network.ID)
	w.WriteHeader(http.StatusNoContent)
}
