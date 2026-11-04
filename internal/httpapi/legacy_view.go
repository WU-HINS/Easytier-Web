package httpapi

import (
	"encoding/json"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/legacyview"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
)

// The original console exposed a device-first view (machine -> instances). The
// handlers below translate it through internal/legacyview so the compiler and
// the session layer never learn about that second model.

func (s *Server) legacyView() *legacyview.Service {
	return legacyview.New(s.cfg.Sessions)
}

// writeProto renders a protobuf response as JSON, keeping the field names the
// original console used.
func writeProto(w http.ResponseWriter, message proto.Message) {
	encoded, err := protojson.Marshal(message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot encode response")
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(encoded))
}

// handleLegacyCollectInfo serves both the whole-device and single-instance form.
func (s *Server) handleLegacyCollectInfo(w http.ResponseWriter, r *http.Request, user bal.User) {
	var ids []string
	if instanceID := r.PathValue("instanceID"); instanceID != "" {
		ids = append(ids, instanceID)
	}
	response, err := s.legacyView().CollectNetworkInfo(r.Context(), user.ID, r.PathValue("machineID"), ids)
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeProto(w, response)
}

// handleLegacyMetas answers instance metadata for a device.
func (s *Server) handleLegacyMetas(w http.ResponseWriter, r *http.Request, user bal.User) {
	// A map keeps the handler free of struct tags; the only field that matters is
	// the id list, and an unknown one is ignored the way the original did.
	var payload map[string][]string
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	response, err := s.legacyView().ListNetworkInstanceMeta(r.Context(), user.ID, r.PathValue("machineID"), payload["instance_ids"])
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeProto(w, response)
}

// handleLegacyGetConfig returns the config a device is running.
func (s *Server) handleLegacyGetConfig(w http.ResponseWriter, r *http.Request, user bal.User) {
	response, err := s.legacyView().GetNetworkInstanceConfig(r.Context(), user.ID, r.PathValue("machineID"), r.PathValue("instanceID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeProto(w, response)
}

// handleLegacyDeleteInstance stops one instance on a device.
func (s *Server) handleLegacyDeleteInstance(w http.ResponseWriter, r *http.Request, user bal.User) {
	if err := s.legacyView().DeleteNetworkInstance(r.Context(), user.ID, r.PathValue("machineID"), r.PathValue("instanceID")); err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLegacyRunInstance starts an instance on a device with a config the
// caller supplies, the way the original console's device view did.
func (s *Server) handleLegacyRunInstance(w http.ResponseWriter, r *http.Request, user bal.User) {
	cfg, ok := s.decodeLegacyConfig(w, r)
	if !ok {
		return
	}
	if err := s.legacyView().RunNetworkInstance(r.Context(), user.ID, r.PathValue("machineID"), cfg, true); err != nil {
		writeError(w, http.StatusBadGateway, "device refused the instance: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLegacyValidateConfig asks the device whether it would accept a config.
func (s *Server) handleLegacyValidateConfig(w http.ResponseWriter, r *http.Request, user bal.User) {
	cfg, ok := s.decodeLegacyConfig(w, r)
	if !ok {
		return
	}
	response, err := s.legacyView().ValidateConfig(r.Context(), user.ID, r.PathValue("machineID"), cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeProto(w, response)
}

// decodeLegacyConfig reads the config object out of the request body.
func (s *Server) decodeLegacyConfig(w http.ResponseWriter, r *http.Request) (*manage.NetworkConfig, bool) {
	var payload map[string]json.RawMessage
	if err := decodeJSON(w, r, &payload); err != nil {
		return nil, false
	}
	raw, ok := payload["config"]
	if !ok {
		writeError(w, http.StatusBadRequest, "config is required")
		return nil, false
	}
	cfg := &manage.NetworkConfig{}
	if err := protojson.Unmarshal(raw, cfg); err != nil {
		writeError(w, http.StatusBadRequest, "config does not parse: "+err.Error())
		return nil, false
	}
	return cfg, true
}
