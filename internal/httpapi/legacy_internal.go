package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
)

// The internal routes are the same device view with the user named in the path,
// which is how the original console's own frontend reached another tenant. They
// reuse the adapters rather than growing a second implementation.

// legacyInternalUser reads the user id from the path.
func legacyInternalUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "user id must be a number")
		return 0, false
	}
	return userID, true
}

// handleInternalCollectInfo mirrors the device view for a named user.
func (s *Server) handleInternalCollectInfo(w http.ResponseWriter, r *http.Request, _ bal.User) {
	userID, ok := legacyInternalUser(w, r)
	if !ok {
		return
	}
	var ids []string
	if instanceID := r.PathValue("instanceID"); instanceID != "" {
		ids = append(ids, instanceID)
	}
	response, err := s.legacyView().CollectNetworkInfo(r.Context(), userID, r.PathValue("machineID"), ids)
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeProto(w, response)
}

// handleInternalRunInstance starts an instance for a named user.
func (s *Server) handleInternalRunInstance(w http.ResponseWriter, r *http.Request, _ bal.User) {
	userID, ok := legacyInternalUser(w, r)
	if !ok {
		return
	}
	cfg, ok := s.decodeLegacyConfig(w, r)
	if !ok {
		return
	}
	if err := s.legacyView().RunNetworkInstance(r.Context(), userID, r.PathValue("machineID"), cfg, true); err != nil {
		writeError(w, http.StatusBadGateway, "device refused the instance: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleInternalDeleteInstance stops an instance for a named user.
func (s *Server) handleInternalDeleteInstance(w http.ResponseWriter, r *http.Request, _ bal.User) {
	userID, ok := legacyInternalUser(w, r)
	if !ok {
		return
	}
	if err := s.legacyView().DeleteNetworkInstance(r.Context(), userID, r.PathValue("machineID"), r.PathValue("instanceID")); err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// proxyMethods maps the method names the original console proxied onto the
// indexes this console uses. A name outside the table is a client error, never a
// forwarded call.
var proxyMethods = map[string]struct {
	method uint32
	build  func() proto.Message
}{
	"ValidateConfig":           {central.MethodValidateConfig, func() proto.Message { return &manage.ValidateConfigRequest{} }},
	"RunNetworkInstance":       {central.MethodRunNetworkInstance, func() proto.Message { return &manage.RunNetworkInstanceRequest{} }},
	"RetainNetworkInstance":    {central.MethodRetainNetworkInstance, func() proto.Message { return &manage.RetainNetworkInstanceRequest{} }},
	"CollectNetworkInfo":       {central.MethodCollectNetworkInfo, func() proto.Message { return &manage.CollectNetworkInfoRequest{} }},
	"DeleteNetworkInstance":    {central.MethodDeleteNetworkInstance, func() proto.Message { return &manage.DeleteNetworkInstanceRequest{} }},
	"GetNetworkInstanceConfig": {central.MethodGetNetworkInstanceCfg, func() proto.Message { return &manage.GetNetworkInstanceConfigRequest{} }},
	"ListNetworkInstanceMeta":  {central.MethodListNetworkInstanceMeta, func() proto.Message { return &manage.ListNetworkInstanceMetaRequest{} }},
}

// handleInternalProxyRPC forwards one call to the device, the way the original
// console let its frontend reach any client method.
func (s *Server) handleInternalProxyRPC(w http.ResponseWriter, r *http.Request, _ bal.User) {
	userID, ok := legacyInternalUser(w, r)
	if !ok {
		return
	}
	var payload map[string]json.RawMessage
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	var serviceName, methodName string
	if raw, ok := payload["service_name"]; ok {
		_ = json.Unmarshal(raw, &serviceName)
	}
	if raw, ok := payload["method_name"]; ok {
		_ = json.Unmarshal(raw, &methodName)
	}
	if serviceName != "api.manage.WebClientService" && serviceName != "WebClientService" {
		writeError(w, http.StatusBadRequest, "only the client service can be proxied")
		return
	}
	entry, ok := proxyMethods[methodName]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown method: "+methodName)
		return
	}
	request := entry.build()
	if raw, ok := payload["payload"]; ok && len(raw) > 0 {
		if err := protojson.Unmarshal(raw, request); err != nil {
			writeError(w, http.StatusBadRequest, "payload does not parse: "+err.Error())
			return
		}
	}
	body, err := s.cfg.Sessions.Call(r.Context(), userID, r.PathValue("machineID"), central.WebClientKey, entry.method, request)
	if err != nil {
		writeError(w, http.StatusBadGateway, "device did not answer: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": json.RawMessage(body)})
}

// handleGatewayInfo reports the gateway network, if any, in the shape the
// original console used: enabled, the peer url and whether data is relayed.
func (s *Server) handleGatewayInfo(w http.ResponseWriter, r *http.Request, user bal.User) {
	networks, err := s.cfg.Store.ListNetworks(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list networks")
		return
	}
	for _, network := range networks {
		if network.NetworkingMethod != "Gateway" || network.PublicServerURL == nil {
			continue
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":    true,
			"peer_url":   *network.PublicServerURL,
			"relay_data": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "peer_url": "", "relay_data": false})
}
