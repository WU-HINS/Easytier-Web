package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// handleUpdateNetwork patches the mutable part of a network intent. The 2.7.0
// console edits networks in place, so the endpoint has to exist for the feature
// to be usable at all.
func (s *Server) handleUpdateNetwork(w http.ResponseWriter, r *http.Request, user bal.User) {
	networkID := r.PathValue("networkID")
	network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, networkID)
	if err != nil {
		writeError(w, http.StatusNotFound, "network not found")
		return
	}
	var payload struct {
		DisplayName      *string   "json:\"display_name\""
		NetworkName      *string   "json:\"network_name\""
		NetworkingMethod *string   "json:\"networking_method\""
		PublicServerURL  *string   "json:\"public_server_url\""
		PeerURLs         *[]string "json:\"peer_urls\""
		VirtualCIDR      *string   "json:\"virtual_cidr\""
		SecureMode       *bool     "json:\"secure_mode\""
		ACLPolicy        *string   "json:\"acl_policy\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	if payload.DisplayName != nil {
		if strings.TrimSpace(*payload.DisplayName) == "" {
			writeFieldError(w, http.StatusBadRequest, fieldError{Code: codeFieldRequired, Field: "display_name", Msg: "display_name must not be empty"})
			return
		}
		network.DisplayName = *payload.DisplayName
	}
	if payload.NetworkName != nil {
		if strings.TrimSpace(*payload.NetworkName) == "" {
			writeFieldError(w, http.StatusBadRequest, fieldError{Code: codeFieldRequired, Field: "network_name", Msg: "network_name must not be empty"})
			return
		}
		network.NetworkName = *payload.NetworkName
	}
	if payload.NetworkingMethod != nil {
		switch *payload.NetworkingMethod {
		case "PublicServer", "Manual", "Standalone", "Gateway":
			network.NetworkingMethod = *payload.NetworkingMethod
		default:
			writeFieldError(w, http.StatusBadRequest, fieldError{Code: codeUnknownMethod, Field: "networking_method", Msg: "unknown networking_method"})
			return
		}
	}
	if payload.PublicServerURL != nil {
		network.PublicServerURL = payload.PublicServerURL
	}
	if payload.PeerURLs != nil {
		network.PeerURLs = *payload.PeerURLs
	}
	if payload.VirtualCIDR != nil {
		network.VirtualCIDR = payload.VirtualCIDR
	}
	if payload.SecureMode != nil {
		network.SecureMode = *payload.SecureMode
	}
	if payload.ACLPolicy != nil {
		// Validate before storing: an unparsable policy makes the compile step
		// fail, and a failing compile silently stops every push for the tenant.
		if err := validateACL(payload.ACLPolicy); err != nil {
			writeError(w, http.StatusBadRequest, "acl_policy does not parse: "+err.Error())
			return
		}
		network.ACLPolicy = payload.ACLPolicy
	}
	if err := validateNetworkIntent(network); err != nil {
		writeValidationError(w, http.StatusBadRequest, err)
		return
	}
	network.UpdateTime = s.cfg.Now()
	if err := s.cfg.Store.UpdateNetwork(r.Context(), network); err != nil {
		if errors.Is(err, bal.ErrNotFound) {
			writeError(w, http.StatusNotFound, "network not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "cannot update network")
		return
	}
	log.Sub("Network").Tag("UPDATE").Info("network updated " + network.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"network_id":        network.ID,
		"display_name":      network.DisplayName,
		"network_name":      network.NetworkName,
		"networking_method": network.NetworkingMethod,
		"secure_mode":       network.SecureMode,
	})
}

// handleUpdateMember patches a membership (hostname override, address, or the
// free form override blob). The member is addressed by device id, which is what
// the 2.7.0 console uses.
func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request, user bal.User) {
	networkID := r.PathValue("networkID")
	deviceID := r.PathValue("deviceID")
	member, err := s.cfg.Store.GetMemberByDevice(r.Context(), user.ID, networkID, deviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	var payload struct {
		HostnameOverride *string "json:\"hostname_override\""
		VirtualIPv4      *string "json:\"virtual_ipv4\""
		OverrideConfig   *string "json:\"override_config\""
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		return
	}
	// Only the fields the caller mentioned are written: a full row write would
	// silently undo a concurrent edit to another column (review team, r140).
	patch := bal.MemberPatch{}
	if payload.HostnameOverride != nil {
		trimmed := strings.TrimSpace(*payload.HostnameOverride)
		if trimmed == "" {
			patch.ClearHostname = boolPtr(true)
		} else {
			if len(trimmed) > 64 {
				writeError(w, http.StatusBadRequest, "hostname_override is too long")
				return
			}
			patch.HostnameOverride = &trimmed
		}
	}
	if payload.VirtualIPv4 != nil {
		trimmed := strings.TrimSpace(*payload.VirtualIPv4)
		if trimmed == "" {
			patch.ClearIPv4 = boolPtr(true)
		} else {
			patch.VirtualIPv4 = &trimmed
			if network, err := s.cfg.Store.GetNetwork(r.Context(), user.ID, networkID); err != nil {
				writeError(w, http.StatusNotFound, "network not found")
				return
			} else if err := validateMemberAddress(network.VirtualCIDR, trimmed); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if payload.OverrideConfig != nil {
		// The same column has a second write path (PUT .../config) that validates;
		// skipping it here let an unparsable blob stop every push for the whole
		// tenant (review team, round 150).
		if err := validateOverrides(payload.OverrideConfig); err != nil {
			writeError(w, http.StatusBadRequest, "override_config does not parse: "+err.Error())
			return
		}
		patch.OverrideConfig = payload.OverrideConfig
	}
	if err := s.cfg.Store.UpdateMember(r.Context(), user.ID, networkID, member.ID, patch, s.cfg.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot update member")
		return
	}
	log.Sub("Network").Tag("MEMBER").Info("member updated " + member.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveMember unbinds a device from a network.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, user bal.User) {
	networkID := r.PathValue("networkID")
	deviceID := r.PathValue("deviceID")
	member, err := s.cfg.Store.GetMemberByDevice(r.Context(), user.ID, networkID, deviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if err := s.cfg.Store.RemoveMember(r.Context(), user.ID, networkID, member.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot remove member")
		return
	}
	log.Sub("Network").Tag("MEMBER").Info("member removed " + member.ID)
	w.WriteHeader(http.StatusNoContent)
}

func boolPtr(value bool) *bool { return &value }
