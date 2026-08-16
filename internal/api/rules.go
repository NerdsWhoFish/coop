package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// ensureChannel caches a channel before anything references it. Allowlist rows
// carry a foreign key to channel, so approving an unfetched one would fail on
// the constraint rather than on anything a parent could act on.
func (s *Server) ensureChannel(ctx context.Context, familyID uuid.UUID, channelID string) error {
	if !youtube.ValidChannelID(channelID) {
		return badRequest("malformed channel id")
	}

	if _, err := s.deps.Catalog.Channel(ctx, channelID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	client, err := s.youtubeFor(ctx, familyID)
	if err != nil {
		return err
	}

	channels, err := client.Channels(ctx, []string{channelID}, domain.PurposeFeed)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return notFound()
	}
	return s.deps.Catalog.UpsertChannels(ctx, channels)
}

func (s *Server) handleGlobalAllowlist(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	rows, err := s.deps.Rules.GlobalAllowlist(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ChannelID
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(rows))
	for _, row := range rows {
		dto := newChannelDTO(channels[row.ChannelID])
		dto.ID = row.ChannelID
		dto.YouTubeURL = youtubeChannelURL(row.ChannelID)
		approvedAt := row.CreatedAt
		dto.ApprovedAt = &approvedAt
		out = append(out, dto)
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleAllowGlobally(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	channelID, err := channelBody(r)
	if err != nil {
		return err
	}
	if err := s.ensureChannel(r.Context(), p.FamilyID, channelID); err != nil {
		return err
	}
	if err := s.deps.Rules.AllowGlobally(r.Context(), p.FamilyID, channelID, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDisallowGlobally(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := s.deps.Rules.DisallowGlobally(r.Context(), p.FamilyID, r.PathValue("channelId"), p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleChildAllowlist reports effective state, not a raw table read, so a
// parent can see why a child can reach a channel.
func (s *Server) handleChildAllowlist(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	global, err := s.deps.Rules.GlobalAllowlist(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}
	perChild, err := s.deps.Rules.ChildAllowlist(r.Context(), child.ID)
	if err != nil {
		return err
	}
	evaluator, err := s.deps.Rules.Evaluator(r.Context(), p.FamilyID, child.ID)
	if err != nil {
		return err
	}

	type entry struct {
		id     string
		source string
	}
	entries := make([]entry, 0, len(global)+len(perChild))
	seen := make(map[string]struct{}, len(global)+len(perChild))
	for _, row := range global {
		entries = append(entries, entry{id: row.ChannelID, source: "global"})
		seen[row.ChannelID] = struct{}{}
	}
	for _, row := range perChild {
		if _, dup := seen[row.ChannelID]; dup {
			continue
		}
		entries = append(entries, entry{id: row.ChannelID, source: "child"})
	}

	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(entries))
	for _, e := range entries {
		dto := newChannelDTO(channels[e.id])
		dto.ID = e.id
		dto.Source = e.source
		dto.YouTubeURL = youtubeChannelURL(e.id)
		dto.State = channelState(evaluator.Channel(e.id))
		dto.DeniedForChild = evaluator.Channel(e.id) != domain.StateAllowed
		out = append(out, dto)
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleAllowForChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	channelID, err := channelBody(r)
	if err != nil {
		return err
	}
	if err := s.ensureChannel(r.Context(), p.FamilyID, channelID); err != nil {
		return err
	}
	if err := s.deps.Rules.AllowForChild(r.Context(), child.ID, channelID, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDisallowForChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	if err := s.deps.Rules.DisallowForChild(r.Context(), child.ID, r.PathValue("channelId"), p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDenyForChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	channelID := r.PathValue("channelId")
	if err := s.ensureChannel(r.Context(), p.FamilyID, channelID); err != nil {
		return err
	}
	if err := s.deps.Rules.DenyForChild(r.Context(), child.ID, channelID, p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleUndenyForChild(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	if err := s.deps.Rules.UndenyForChild(r.Context(), child.ID, r.PathValue("channelId"), p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleBlocklist(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	rows, err := s.deps.Rules.BlockedChannels(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ChannelID
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(rows))
	for _, row := range rows {
		dto := newChannelDTO(channels[row.ChannelID])
		dto.ID = row.ChannelID
		dto.Reason = row.Reason
		dto.State = channelState(domain.StateBlocked)
		out = append(out, dto)
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleBlockChannel(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}

	var body struct {
		ChannelID string `json:"channelId"`
		Reason    string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if err := s.ensureChannel(r.Context(), p.FamilyID, body.ChannelID); err != nil {
		return err
	}
	if err := s.deps.Rules.BlockChannelForFamily(r.Context(), p.FamilyID, body.ChannelID, body.Reason, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleUnblockChannel(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	if err := p.RequireAdmin(); err != nil {
		return err
	}
	if err := s.deps.Rules.UnblockChannel(r.Context(), p.FamilyID, r.PathValue("channelId"), p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleListKeywords(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	var childID *uuid.UUID
	if raw := r.URL.Query().Get("childId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return badRequest("malformed childId")
		}
		if err := p.RequireChild(parsed); err != nil {
			return err
		}
		childID = &parsed
	}

	rows, err := s.deps.Rules.ListKeywords(r.Context(), p.FamilyID, childID)
	if err != nil {
		return err
	}

	out := make([]keywordDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, newKeywordDTO(row))
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleCreateKeyword(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	var body struct {
		Term             string     `json:"term"`
		ChildID          *uuid.UUID `json:"childId"`
		MatchTitle       *bool      `json:"matchTitle"`
		MatchTags        *bool      `json:"matchTags"`
		MatchDescription *bool      `json:"matchDescription"`
		WholeWord        *bool      `json:"wholeWord"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Term == "" {
		return badRequest("term is required")
	}
	if body.ChildID != nil {
		if err := p.RequireChild(*body.ChildID); err != nil {
			return err
		}
	} else if err := p.RequireAdmin(); err != nil {
		// A family-wide keyword affects every child, including ones a scoped
		// parent cannot see.
		return err
	}

	row := store.Keyword{
		FamilyID:         p.FamilyID,
		ChildID:          body.ChildID,
		Term:             body.Term,
		MatchTitle:       boolOr(body.MatchTitle, true),
		MatchTags:        boolOr(body.MatchTags, true),
		MatchDescription: boolOr(body.MatchDescription, false),
		WholeWord:        boolOr(body.WholeWord, true),
	}

	created, err := s.deps.Rules.CreateKeyword(r.Context(), row, p.ID)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusCreated, newKeywordDTO(created))
	return nil
}

func (s *Server) handleUpdateKeyword(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	keywordID, err := pathUUID(r, "keywordId")
	if err != nil {
		return err
	}

	var body struct {
		Term             *string `json:"term"`
		MatchTitle       *bool   `json:"matchTitle"`
		MatchTags        *bool   `json:"matchTags"`
		MatchDescription *bool   `json:"matchDescription"`
		WholeWord        *bool   `json:"wholeWord"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}

	updates := map[string]any{}
	if body.Term != nil {
		updates["term"] = *body.Term
	}
	if body.MatchTitle != nil {
		updates["match_title"] = *body.MatchTitle
	}
	if body.MatchTags != nil {
		updates["match_tags"] = *body.MatchTags
	}
	if body.MatchDescription != nil {
		updates["match_description"] = *body.MatchDescription
	}
	if body.WholeWord != nil {
		updates["whole_word"] = *body.WholeWord
	}
	if len(updates) == 0 {
		return badRequest("no fields to update")
	}

	if err := s.deps.Rules.UpdateKeyword(r.Context(), p.FamilyID, keywordID, updates, p.ID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDeleteKeyword(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	keywordID, err := pathUUID(r, "keywordId")
	if err != nil {
		return err
	}
	if err := s.deps.Rules.DeleteKeyword(r.Context(), p.FamilyID, keywordID, p.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	children, err := s.deps.Accounts.Children(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	names := make(map[uuid.UUID]string, len(children))
	ids := make([]uuid.UUID, 0, len(children))
	for _, child := range children {
		if p.CanManage(child.ID) {
			ids = append(ids, child.ID)
			names[child.ID] = child.Name
		}
	}

	requests, err := s.deps.Activity.Requests(r.Context(), store.RequestQuery{
		ChildIDs: ids,
		Status:   domain.RequestStatus(r.URL.Query().Get("status")),
		Limit:    queryInt(r, "limit", 50),
	})
	if err != nil {
		return err
	}

	channelIDs := make([]string, len(requests))
	for i, req := range requests {
		channelIDs[i] = req.ChannelID
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), channelIDs)
	if err != nil {
		return err
	}

	out := make([]requestDTO, 0, len(requests))
	for _, req := range requests {
		channel := newChannelDTO(channels[req.ChannelID])
		channel.ID = req.ChannelID
		channel.YouTubeURL = youtubeChannelURL(req.ChannelID)

		out = append(out, requestDTO{
			ID:        req.ID,
			ChildID:   req.ChildID,
			ChildName: names[req.ChildID],
			Channel:   channel,
			Status:    string(req.Status),
			Note:      req.DecisionNote,
			CreatedAt: req.CreatedAt,
			DecidedAt: req.DecidedAt,
		})
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, requestDTO2Page(out))
	return nil
}

func requestDTO2Page(items []requestDTO) map[string]any {
	return map[string]any{"items": items}
}

// handleApproveRequest approves and grants in one step, since an approval that
// did not actually let the child watch anything would be a lie.
func (s *Server) handleApproveRequest(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	request, err := s.requestInScope(r, p)
	if err != nil {
		return err
	}

	var body struct {
		Scope string `json:"scope"`
		Note  string `json:"note"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			return err
		}
	}

	if err := s.ensureChannel(r.Context(), p.FamilyID, request.ChannelID); err != nil {
		return err
	}

	if body.Scope == "global" {
		if err := p.RequireAdmin(); err != nil {
			return err
		}
		err = s.deps.Rules.AllowGlobally(r.Context(), p.FamilyID, request.ChannelID, p.ID)
	} else {
		err = s.deps.Rules.AllowForChild(r.Context(), request.ChildID, request.ChannelID, p.ID)
	}
	if err != nil {
		return err
	}

	if err := s.deps.Activity.DecideRequest(r.Context(), request.ID, p.ID,
		domain.RequestApproved, body.Note); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDenyRequest(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	request, err := s.requestInScope(r, p)
	if err != nil {
		return err
	}

	var body struct {
		Note  string `json:"note"`
		Block bool   `json:"block"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			return err
		}
	}

	if body.Block {
		if err := p.RequireAdmin(); err != nil {
			return err
		}
		if err := s.ensureChannel(r.Context(), p.FamilyID, request.ChannelID); err != nil {
			return err
		}
		if err := s.deps.Rules.BlockChannelForFamily(r.Context(), p.FamilyID,
			request.ChannelID, body.Note, p.ID); err != nil {
			return err
		}
	}

	if err := s.deps.Activity.DecideRequest(r.Context(), request.ID, p.ID,
		domain.RequestDenied, body.Note); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) requestInScope(r *http.Request, p auth.Parent) (store.Request, error) {
	requestID, err := pathUUID(r, "requestId")
	if err != nil {
		return store.Request{}, err
	}

	request, err := s.deps.Activity.Request(r.Context(), requestID)
	if err != nil {
		return store.Request{}, err
	}
	if err := p.RequireChild(request.ChildID); err != nil {
		return store.Request{}, err
	}
	if _, err := s.deps.Accounts.Child(r.Context(), p.FamilyID, request.ChildID); err != nil {
		return store.Request{}, err
	}
	return request, nil
}

func (s *Server) handleListSuppressions(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}

	rows, err := s.deps.Activity.Suppressions(r.Context(), child.ID, queryInt(r, "limit", 50))
	if err != nil {
		return err
	}

	out := make([]suppressionDTO, 0, len(rows))
	for _, row := range rows {
		video, err := s.deps.Catalog.Video(r.Context(), row.VideoID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		out = append(out, suppressionDTO{
			ID:           row.ID,
			Video:        newVideoDTO(video, "", s.deps.Config.Server.PublicURL),
			Term:         row.MatchedTerm,
			MatchedField: row.MatchedField,
			CreatedAt:    row.CreatedAt,
		})
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]any{"items": out})
	return nil
}

func (s *Server) handleOverrideSuppression(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	suppressionID, err := pathUUID(r, "suppressionId")
	if err != nil {
		return err
	}

	suppression, err := s.deps.Activity.Suppression(r.Context(), suppressionID)
	if err != nil {
		return err
	}
	if err := p.RequireChild(suppression.ChildID); err != nil {
		return err
	}

	var body struct {
		Scope string `json:"scope"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			return err
		}
	}

	override := store.VideoOverride{
		FamilyID:  p.FamilyID,
		VideoID:   suppression.VideoID,
		CreatedBy: p.ID,
	}
	if body.Scope != "family" {
		childID := suppression.ChildID
		override.ChildID = &childID
	}

	if err := s.deps.Rules.CreateOverride(r.Context(), override); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleParentSearchChannels(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	query := r.URL.Query().Get("q")
	if query == "" {
		return badRequest("q is required")
	}

	client, err := s.youtubeFor(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}

	channels, err := client.SearchChannels(r.Context(), query)
	if err != nil {
		return err
	}
	if err := s.deps.Catalog.UpsertChannels(r.Context(), channels); err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(channels))
	for _, channel := range channels {
		out = append(out, channelDTO{
			ID:              channel.ID,
			Title:           channel.Title,
			Description:     channel.Description,
			ThumbnailURL:    channel.ThumbnailURL,
			SubscriberCount: channel.SubscriberCount,
			YouTubeURL:      youtubeChannelURL(channel.ID),
		})
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func channelBody(r *http.Request) (string, error) {
	var body struct {
		ChannelID string `json:"channelId"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return "", err
	}
	if body.ChannelID == "" {
		return "", badRequest("channelId is required")
	}
	return body.ChannelID, nil
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
