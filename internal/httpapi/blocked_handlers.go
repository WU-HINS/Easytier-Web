package httpapi

import (
	"errors"
	"net/http"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// handleListBlockedDevices exposes the block list. Without it a block created by
// DELETE /machines/{id}?block=true was a permanent, invisible one way door: no
// way to see it, no way to lift it.
func (s *Server) handleListBlockedDevices(w http.ResponseWriter, r *http.Request, user bal.User) {
	blocked, err := s.cfg.Store.ListBlockedDevices(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list blocked devices")
		return
	}
	items := make([]map[string]any, 0, len(blocked))
	for _, item := range blocked {
		entry := map[string]any{
			"machine_id":    item.MachineID,
			"hostname":      item.Hostname,
			"blocked_time":  item.BlockedTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"attempt_count": item.AttemptCount,
		}
		if item.LastAttemptTime != nil {
			entry["last_attempt_time"] = item.LastAttemptTime.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		items = append(items, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"blocked": items})
}

// handleUnblockDevice lifts a block so the device may reconnect.
func (s *Server) handleUnblockDevice(w http.ResponseWriter, r *http.Request, user bal.User) {
	if err := s.cfg.Store.UnblockDevice(r.Context(), user.ID, r.PathValue("machineID")); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "blocked device not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot unblock device")
		return
	}
	log.Sub("Device").Tag("UNBLOCK").Info("device unblocked " + r.PathValue("machineID"))
	w.WriteHeader(http.StatusNoContent)
}
