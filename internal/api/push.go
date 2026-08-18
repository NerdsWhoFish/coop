package api

import (
	"net/http"

	"github.com/nerdswhofish/coop/internal/auth"
)

// pushTokenBody carries one APNs device token. Registration is accepted even
// when delivery is disabled, so enabling APNs later needs no re-registration
// from installed apps.
type pushTokenBody struct {
	Token string `json:"token"`
}

func (b pushTokenBody) validate() error {
	if b.Token == "" {
		return badRequest("token is required")
	}
	if len(b.Token) > 512 {
		return badRequest("token is too long")
	}
	return nil
}

func (s *Server) handleSaveParentPushToken(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	var body pushTokenBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if err := body.validate(); err != nil {
		return err
	}
	if err := s.deps.Accounts.SaveParentPushToken(r.Context(), p.FamilyID, p.ID,
		body.Token); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDeleteParentPushToken(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	token := r.PathValue("token")
	if token == "" {
		return badRequest("token is required")
	}
	if err := s.deps.Accounts.DeleteParentPushToken(r.Context(), p.ID, token); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleSaveChildPushToken(w http.ResponseWriter, r *http.Request,
	c auth.Child) error {

	var body pushTokenBody
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if err := body.validate(); err != nil {
		return err
	}
	if err := s.deps.Accounts.SaveChildPushToken(r.Context(), c.FamilyID, c.ID, c.DeviceID,
		body.Token); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
