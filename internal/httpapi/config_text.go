package httpapi

import (
	"encoding/json"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/easytier/easytier-console/internal/configtext"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
)

// The original console converts between the pushed config and TOML text in the
// browser. Both endpoints answer 200 with an error field instead of a status
// code, so a client can show the parse error next to the editor.

// handleGenerateConfig renders a pushed config as TOML.
func (s *Server) handleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Config json.RawMessage `json:"config"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	cfg := &manage.NetworkConfig{}
	if len(payload.Config) > 0 {
		if err := protojson.Unmarshal(payload.Config, cfg); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": "config does not parse: " + err.Error()})
			return
		}
	}
	text, err := configtext.FromNetworkConfig(cfg).Dump()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"toml_config": text})
}

// handleParseConfig turns TOML text back into a pushed config.
func (s *Server) handleParseConfig(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		TomlConfig string `json:"toml_config"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	doc, err := configtext.Parse(payload.TomlConfig)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": "toml does not parse: " + err.Error()})
		return
	}
	encoded, err := protojson.Marshal(doc.ToNetworkConfig())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": json.RawMessage(encoded)})
}

// handleGenerateConfigLite adapts the shared handler to the authenticated route
// signature.
func (s *Server) handleGenerateConfigLite(w http.ResponseWriter, r *http.Request, _ bal.User) {
	s.handleGenerateConfig(w, r)
}

// handleParseConfigLite adapts the shared handler to the authenticated route
// signature.
func (s *Server) handleParseConfigLite(w http.ResponseWriter, r *http.Request, _ bal.User) {
	s.handleParseConfig(w, r)
}
