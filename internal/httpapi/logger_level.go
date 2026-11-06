package httpapi

import (
	"net/http"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/legacyview"
)

// handleGetLoggerLevel reads the log level of the device process. The level is
// per process, not per instance: every instance on that device shares it.
func (s *Server) handleGetLoggerLevel(w http.ResponseWriter, r *http.Request, user bal.User) {
	level, err := s.legacyView().GetLoggerLevel(r.Context(), user.ID, r.PathValue("machineID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"level": legacyview.LoggerLevelName(level),
		"value": level,
	})
}

// handlePutLoggerLevel sets the level. An unknown name is a client error rather
// than a silent default, because picking the wrong level changes what is logged.
func (s *Server) handlePutLoggerLevel(w http.ResponseWriter, r *http.Request, user bal.User) {
	var payload struct {
		Level string `json:"level"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	value, ok := legacyview.LoggerLevelValue(payload.Level)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown log level")
		return
	}
	if err := s.legacyView().SetLoggerLevel(r.Context(), user.ID, r.PathValue("machineID"), value); err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"level": payload.Level, "value": value})
}
