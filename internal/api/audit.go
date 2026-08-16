package api

import (
	"net/http"
	"time"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/store"
)

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request, parent auth.Parent) error {
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return badRequest("before must be an RFC 3339 timestamp")
		}
		before = parsed
	}
	events, err := s.deps.Audit.Events(r.Context(), store.AuditQuery{
		FamilyID: parent.FamilyID, ChildIDs: parent.ScopedChildIDs,
		IncludeGlobal: parent.IsAdmin(), Before: before, Limit: queryInt(r, "limit", 50),
	})
	if err != nil {
		return err
	}
	out := make([]auditEventDTO, len(events))
	for i, event := range events {
		out[i] = newAuditEventDTO(event)
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}
