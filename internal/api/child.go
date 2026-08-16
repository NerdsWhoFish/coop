package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// handlePair redeems a pairing code for a device token.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}

	code := auth.NormalizePairingCode(body.Code)
	if code == "" {
		return badRequest("that pairing code is not valid")
	}
	if body.DeviceName == "" {
		body.DeviceName = "Child device"
	}

	token, err := auth.NewToken()
	if err != nil {
		return internal(err)
	}

	child, _, err := s.deps.Accounts.RedeemPairingCode(r.Context(), code, body.DeviceName, token.Hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Unknown, expired and already-used all read the same, so a code
			// cannot be probed for.
			return badRequest("that pairing code is not valid")
		}
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]any{
		"token": token.Plain,
		"child": newChildProfileDTO(child),
	})
	return nil
}

func (s *Server) handleChildMe(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	child, err := s.deps.Accounts.Child(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, newChildProfileDTO(child))
	return nil
}

func (s *Server) handleChildFeed(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	page, err := s.deps.Feed.Home(r.Context(), c.FamilyID, c.ID,
		queryInt(r, "limit", 30), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}

	items, err := s.videoDTOs(r, page.Videos)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, videoPageDTO{
		Items:      items,
		NextCursor: page.NextCursor,
	})
	return nil
}

func (s *Server) handleChildShorts(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	child, err := s.deps.Accounts.Child(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}
	if !child.ShortsEnabled {
		// A disabled surface reports 404 rather than 403, so the tab simply
		// does not exist for this child.
		return notFound()
	}

	page, err := s.deps.Feed.Shorts(r.Context(), c.FamilyID, c.ID,
		r.URL.Query().Get("session"), queryInt(r, "limit", 10), queryInt(r, "offset", 0))
	if err != nil {
		return err
	}

	items, err := s.videoDTOs(r, page.Videos)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, videoPageDTO{Items: items})
	return nil
}

func (s *Server) handleChildSubscriptions(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	ids, err := s.deps.Activity.SubscribedChannelIDs(r.Context(), c.ID)
	if err != nil {
		return err
	}

	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return err
	}
	evaluator, err := s.deps.Rules.Evaluator(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(ids))
	for _, id := range ids {
		state := evaluator.Channel(id)
		if state == domain.StateBlocked {
			// A channel blocked after the child subscribed simply vanishes.
			continue
		}
		dto := newChannelDTO(channels[id])
		dto.ID = id
		dto.State = channelState(state)
		out = append(out, dto)
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	channelID := r.PathValue("channelId")

	evaluator, err := s.deps.Rules.Evaluator(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}
	if evaluator.Channel(channelID) == domain.StateBlocked {
		return notFound()
	}
	if _, err := s.deps.Catalog.Channel(r.Context(), channelID); err != nil {
		return err
	}
	if err := s.deps.Activity.Subscribe(r.Context(), c.ID, channelID); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	if err := s.deps.Activity.Unsubscribe(r.Context(), c.ID, r.PathValue("channelId")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleChildChannel(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	view, err := s.deps.Feed.Channel(r.Context(), c.FamilyID, c.ID,
		r.PathValue("channelId"), queryInt(r, "limit", 30), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}

	items, err := s.videoDTOs(r, view.Videos)
	if err != nil {
		return err
	}

	channel := newChannelDTO(view.Channel)
	channel.State = channelState(view.State)

	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]any{
		"channel":        channel,
		"state":          channelState(view.State),
		"subscribed":     view.Subscribed,
		"pendingRequest": view.PendingRequest,
		"videos":         items,
		"nextCursor":     view.NextCursor,
	})
	return nil
}

// handleChildSearch returns channels, and videos when the child's settings
// allow it, marking anything not yet approved so the client can offer an ask
// rather than a dead end.
func (s *Server) handleChildSearch(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	query := youtube.NormalizeQuery(r.URL.Query().Get("q"))
	if query == "" {
		return badRequest("q is required")
	}

	child, err := s.deps.Accounts.Child(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}

	day := youtube.QuotaDay(s.deps.Now())
	if child.DailySearchLimit > 0 {
		used, err := s.deps.Activity.SearchCount(r.Context(), c.ID, day)
		if err != nil {
			return err
		}
		if used >= child.DailySearchLimit {
			return tooManyRequests("search_limit",
				"you have used all of today's searches")
		}
	}

	client, err := s.youtubeFor(r.Context(), c.FamilyID)
	if err != nil {
		return err
	}

	var channels []youtube.Channel
	var videos []store.Video
	if child.VideoSearchTiles {
		results, err := client.Search(r.Context(), query)
		if err != nil {
			return err
		}
		channels = results.Channels
		if err := s.deps.Catalog.UpsertChannels(r.Context(), results.RelatedChannels); err != nil {
			return err
		}
		if err := s.deps.Catalog.UpsertVideos(r.Context(), results.Videos); err != nil {
			return err
		}

		ids := make([]string, len(results.Videos))
		for i, video := range results.Videos {
			ids[i] = video.ID
		}
		videos, err = s.deps.Catalog.VideosByID(r.Context(), ids)
		if err != nil {
			return err
		}
	} else {
		channels, err = client.SearchChannels(r.Context(), query)
		if err != nil {
			return err
		}
		if err := s.deps.Catalog.UpsertChannels(r.Context(), channels); err != nil {
			return err
		}
	}
	if err := s.deps.Activity.RecordSearch(r.Context(), c.ID, day); err != nil {
		return err
	}

	evaluator, err := s.deps.Rules.Evaluator(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}

	out := make([]channelDTO, 0, len(channels))
	for _, channel := range channels {
		state := evaluator.Channel(channel.ID)
		if state == domain.StateBlocked {
			// Blocked channels never appear, so a search cannot reveal that
			// one exists.
			continue
		}
		out = append(out, channelDTO{
			ID:              channel.ID,
			Title:           channel.Title,
			Description:     channel.Description,
			ThumbnailURL:    channel.ThumbnailURL,
			SubscriberCount: channel.SubscriberCount,
			State:           channelState(state),
		})
	}

	videoOut := []videoDTO{}
	if len(videos) > 0 {
		videoResults, err := s.deps.Feed.Search(r.Context(), c.FamilyID, c.ID, videos)
		if err != nil {
			return err
		}
		videoRows := make([]store.Video, len(videoResults))
		for i, result := range videoResults {
			videoRows[i] = result.Video
		}
		videoOut, err = s.videoDTOs(r, videoRows)
		if err != nil {
			return err
		}
		for i, result := range videoResults {
			videoOut[i].Locked = result.Locked
		}
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]any{
		"channels": out,
		"videos":   videoOut,
	})
	return nil
}

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	video, err := s.deps.Feed.Watchable(r.Context(), c.FamilyID, c.ID, r.PathValue("videoId"))
	if err != nil {
		return err
	}

	child, err := s.deps.Accounts.Child(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}

	channel, err := s.deps.Catalog.Channel(r.Context(), video.ChannelID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	reaction, _, err := s.deps.Activity.Reaction(r.Context(), c.ID, video.ID)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, watchPageDTO{
		Video:    newVideoDTO(video, channel.Title, s.deps.Config.Server.PublicURL),
		EmbedURL: youtube.EmbedURL(video.ID, child.WatchPageAutoplay),
		Autoplay: child.WatchPageAutoplay,
		Reaction: string(reaction),
		ShareURL: youtube.WatchURL(video.ID),
	})
	return nil
}

func (s *Server) handleSetReaction(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	videoID := r.PathValue("videoId")
	if _, err := s.deps.Feed.Watchable(r.Context(), c.FamilyID, c.ID, videoID); err != nil {
		return err
	}

	var body struct {
		Kind string `json:"kind"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}

	kind := domain.ReactionKind(body.Kind)
	if kind != domain.ReactionLike && kind != domain.ReactionDislike {
		return badRequest("kind must be like or dislike")
	}
	if err := s.deps.Activity.SetReaction(r.Context(), c.ID, videoID, kind); err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleClearReaction(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	if err := s.deps.Activity.ClearReaction(r.Context(), c.ID, r.PathValue("videoId")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleRecordWatch(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	videoID := r.PathValue("videoId")
	video, err := s.deps.Feed.Watchable(r.Context(), c.FamilyID, c.ID, videoID)
	if err != nil {
		return err
	}

	var body struct {
		StartedAt      time.Time `json:"startedAt"`
		SecondsWatched int       `json:"secondsWatched"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.StartedAt.IsZero() {
		body.StartedAt = s.deps.Now()
	}

	err = s.deps.Activity.RecordWatch(r.Context(), c.ID, videoID,
		body.StartedAt, body.SecondsWatched, video.DurationSeconds)
	if err != nil {
		return err
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleChildRequests(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	requests, err := s.deps.Activity.ChildRequests(r.Context(), c.ID, queryInt(r, "limit", 50))
	if err != nil {
		return err
	}

	ids := make([]string, len(requests))
	for i, req := range requests {
		ids[i] = req.ChannelID
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return err
	}

	out := make([]requestDTO, 0, len(requests))
	for _, req := range requests {
		channel := newChannelDTO(channels[req.ChannelID])
		channel.ID = req.ChannelID
		out = append(out, requestDTO{
			ID:        req.ID,
			ChildID:   req.ChildID,
			Channel:   channel,
			Status:    string(req.Status),
			CreatedAt: req.CreatedAt,
			DecidedAt: req.DecidedAt,
		})
	}

	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

// handleRaiseRequest records a child asking for a channel. A blocked channel
// reports 404, so asking cannot confirm one exists.
func (s *Server) handleRaiseRequest(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	var body struct {
		ChannelID         string  `json:"channelId"`
		PromptedByVideoID *string `json:"promptedByVideoId"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.ChannelID == "" {
		return badRequest("channelId is required")
	}

	evaluator, err := s.deps.Rules.Evaluator(r.Context(), c.FamilyID, c.ID)
	if err != nil {
		return err
	}
	switch evaluator.Channel(body.ChannelID) {
	case domain.StateBlocked:
		return notFound()
	case domain.StateAllowed:
		return conflict("already_allowed", "this channel is already approved")
	}

	if _, err := s.deps.Catalog.Channel(r.Context(), body.ChannelID); err != nil {
		return err
	}

	request, err := s.deps.Activity.RaiseRequest(r.Context(), c.ID, body.ChannelID, body.PromptedByVideoID)
	if err != nil {
		return err
	}

	writeJSON(w, s.deps.Logger, http.StatusCreated, requestDTO{
		ID:        request.ID,
		ChildID:   request.ChildID,
		Channel:   channelDTO{ID: body.ChannelID},
		Status:    string(request.Status),
		CreatedAt: request.CreatedAt,
	})
	return nil
}

// videoDTOs decorates videos with their channel titles in one lookup, rather
// than a query per row.
func (s *Server) videoDTOs(r *http.Request, videos []store.Video) ([]videoDTO, error) {
	if len(videos) == 0 {
		return []videoDTO{}, nil
	}

	ids := make([]string, 0, len(videos))
	seen := make(map[string]struct{}, len(videos))
	for _, v := range videos {
		if _, dup := seen[v.ChannelID]; dup {
			continue
		}
		seen[v.ChannelID] = struct{}{}
		ids = append(ids, v.ChannelID)
	}

	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), ids)
	if err != nil {
		return nil, err
	}

	out := make([]videoDTO, len(videos))
	for i, v := range videos {
		out[i] = newVideoDTO(v, channels[v.ChannelID].Title, s.deps.Config.Server.PublicURL)
	}
	return out, nil
}
