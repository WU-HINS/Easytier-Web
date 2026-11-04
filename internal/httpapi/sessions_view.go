package httpapi

import (
	"net/http"
	"strconv"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/session"
)

// The original console lists live device sessions and can drop one. Both reports
// come from the session manager, so the answer always matches what is really
// connected rather than what the database remembers.

// handleListSessions answers with the caller's own sessions; an admin sees every
// session, which is what the original management page showed.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, user bal.User) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessionSnapshots(user)})
}

// handleListSessionsInternal is the same report for the internal route.
func (s *Server) handleListSessionsInternal(w http.ResponseWriter, r *http.Request, user bal.User) {
	s.handleListSessions(w, r, user)
}

// sessionSnapshots collects snapshots for one user, or for everyone when the
// caller is an admin.
func (s *Server) sessionSnapshots(user bal.User) []map[string]any {
	owners := []int64{user.ID}
	if user.Role == "admin" {
		owners = s.cfg.Sessions.ConnectedUsers()
	}
	out := make([]map[string]any, 0, len(owners))
	for _, owner := range owners {
		for _, snapshot := range s.cfg.Sessions.Snapshots(owner) {
			out = append(out, describeSession(snapshot))
		}
	}
	return out
}

// describeSession renders one snapshot in the original field names.
func describeSession(snapshot session.Snapshot) map[string]any {
	return map[string]any{
		"machine_id": snapshot.MachineID,
		"user_id":    snapshot.UserID,
		"hostname":   snapshot.Hostname,
		"version":    snapshot.Version,
		"client_url": snapshot.ClientURL,
		"started_at": snapshot.StartedAt,
		"last_seen":  snapshot.LastSeen,
		"online":     snapshot.Online,
		"instances":  snapshot.Instances,
		"failed":     snapshot.Failed,
	}
}

// handleDisconnectSessionInternal drops one device session, the internal route
// the original console used to kick a machine.
func (s *Server) handleDisconnectSessionInternal(w http.ResponseWriter, r *http.Request, _ bal.User) {
	userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "user id must be a number")
		return
	}
	closed := s.cfg.Sessions.DisconnectDevice(userID, r.PathValue("machineID"))
	writeJSON(w, http.StatusOK, map[string]any{"closed": closed})
}
