package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
)

// validateACL keeps a malformed policy out of the database: an unparsable policy
// makes the compile step fail, and a failing compile stops every push for that
// tenant (visible only as "reconcile failed" in the log).
func validateACL(raw *string) error {
	if raw == nil {
		return nil
	}
	_, err := central.ParsePolicy(raw)
	return err
}

// validateOverrides does the same for the per member override blob.
func validateOverrides(raw *string) error {
	if raw == nil {
		return nil
	}
	_, err := central.ParseOverrides(raw)
	return err
}

// handleGetACLPolicy returns the stored policy verbatim.
func (s *Server) handleGetACLPolicy(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	policy := ""
	if network.ACLPolicy != nil {
		policy = *network.ACLPolicy
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl_policy": policy})
}

// handlePutACLPolicy replaces the policy after validating it.
func (s *Server) handlePutACLPolicy(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	var payload struct {
		ACLPolicy string "json:\"acl_policy\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	raw := strings.TrimSpace(payload.ACLPolicy)
	var pointer *string
	if raw != "" {
		if err := validateACL(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "acl_policy does not parse: "+err.Error())
			return
		}
		pointer = &raw
	}
	network.ACLPolicy = pointer
	network.UpdateTime = s.cfg.Now()
	if err := s.cfg.Store.UpdateNetwork(r.Context(), network); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update acl policy")
		return
	}
	log.Sub("Network").Tag("ACL").Info("acl policy updated " + network.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleGetMemberConfig returns the raw override blob for one membership.
func (s *Server) handleGetMemberConfig(w http.ResponseWriter, r *http.Request, user bal.User) {
	member, err := s.cfg.Store.GetMemberByDevice(r.Context(), user.ID, r.PathValue("networkID"), r.PathValue("deviceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	override := ""
	if member.OverrideConfig != nil {
		override = *member.OverrideConfig
	}
	writeJSON(w, http.StatusOK, map[string]any{"override_config": override})
}

// handlePutMemberConfig replaces the override blob after validating it.
func (s *Server) handlePutMemberConfig(w http.ResponseWriter, r *http.Request, user bal.User) {
	member, err := s.cfg.Store.GetMemberByDevice(r.Context(), user.ID, r.PathValue("networkID"), r.PathValue("deviceID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	var payload struct {
		OverrideConfig string "json:\"override_config\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	raw := strings.TrimSpace(payload.OverrideConfig)
	if raw != "" {
		if err := validateOverrides(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "override_config does not parse: "+err.Error())
			return
		}
	}
	// Only this column is written; a full row write would undo a concurrent
	// edit to another field (review team, r140).
	patch := bal.MemberPatch{OverrideConfig: &raw}
	if err := s.cfg.Store.UpdateMember(r.Context(), user.ID, r.PathValue("networkID"), member.ID, patch, s.cfg.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update member config")
		return
	}
	log.Sub("Network").Tag("MEMBER").Info("member override updated " + member.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleListCredentials lists the network credentials without their secrets.
func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request, user bal.User) {
	// Check ownership first: the listing query is already tenant filtered, but a
	// silent 200 with an empty array hides typos and lets a tenant probe ids.
	if _, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID")); err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	credentials, err := s.cfg.Store.ListCredentials(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list credentials")
		return
	}
	out := make([]map[string]any, 0, len(credentials))
	for _, credential := range credentials {
		// The secret is never returned: it cannot be recovered once lost.
		out = append(out, map[string]any{
			"credential_id":       credential.CredentialID,
			"expiry_unix":         credential.ExpiryUnix,
			"acl_groups":          credential.ACLGroups,
			"allow_relay":         credential.AllowRelay,
			"allowed_proxy_cidrs": credential.AllowedProxyCIDRs,
			"reusable":            credential.Reusable,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// handleCreateCredential issues a new network credential.
func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request, user bal.User) {
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, r.PathValue("networkID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	var payload struct {
		ACLGroups         []string "json:\"acl_groups\""
		AllowRelay        bool     "json:\"allow_relay\""
		AllowedProxyCIDRs []string "json:\"allowed_proxy_cidrs\""
		ExpiryUnix        int64    "json:\"expiry_unix\""
		Reusable          bool     "json:\"reusable\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	secret, err := auth.RandomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate credential")
		return
	}
	now := s.cfg.Now()
	credential := bal.NetworkCredential{
		UserID: user.ID, NetworkID: network.ID, CredentialID: mustRandomUUID(),
		CredentialSecret: secret, ExpiryUnix: payload.ExpiryUnix,
		ACLGroups: payload.ACLGroups, AllowRelay: payload.AllowRelay,
		AllowedProxyCIDRs: payload.AllowedProxyCIDRs, Reusable: payload.Reusable,
		CreateTime: now, UpdateTime: now,
	}
	if err := s.cfg.Store.CreateCredential(r.Context(), credential); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot create credential")
		return
	}
	log.Sub("Network").Tag("CREDENTIAL").Info("credential created " + credential.CredentialID)
	// The secret is returned exactly once, at creation.
	writeJSON(w, http.StatusCreated, map[string]any{
		"credential_id":     credential.CredentialID,
		"credential_secret": credential.CredentialSecret,
	})
}

// handleRevokeCredential deletes a credential.
func (s *Server) handleRevokeCredential(w http.ResponseWriter, r *http.Request, user bal.User) {
	if err := s.cfg.Store.RevokeCredential(r.Context(), user.ID, r.PathValue("networkID"), r.PathValue("credentialID")); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "credential not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot revoke credential")
		return
	}
	log.Sub("Network").Tag("CREDENTIAL").Info("credential revoked " + r.PathValue("credentialID"))
	w.WriteHeader(http.StatusNoContent)
}

var _ = json.Marshal
