package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nerdswhofish/coop/internal/auth"
	"github.com/nerdswhofish/coop/internal/store"
)

const (
	playbackLeaseWindow = 45 * time.Second
	playbackLongPoll    = 20 * time.Second
)

func (s *Server) handlePlaybackLease(w http.ResponseWriter, r *http.Request, c auth.Child) error {
	var body struct {
		VideoID string `json:"videoId"`
		State   string `json:"state"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if strings.TrimSpace(body.VideoID) == "" {
		return badRequest("videoId is required")
	}

	if body.State == "stopped" {
		if err := s.deps.Activity.StopPlayback(r.Context(), c.DeviceID, body.VideoID); err != nil {
			return err
		}
		writeJSON(w, s.deps.Logger, http.StatusOK, map[string]bool{"allowed": true})
		return nil
	}
	if body.State != "started" && body.State != "heartbeat" {
		return badRequest("state must be started, heartbeat, or stopped")
	}

	if _, err := s.deps.Feed.Watchable(r.Context(), c.FamilyID, c.ID, body.VideoID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			trace.SpanFromContext(r.Context()).AddEvent(
				"coop.playback.interrupted",
				trace.WithAttributes(attribute.String("coop.playback.state", body.State)),
			)
			if stopErr := s.deps.Activity.StopPlayback(r.Context(), c.DeviceID, body.VideoID); stopErr != nil {
				return stopErr
			}
			writeJSON(w, s.deps.Logger, http.StatusOK, map[string]bool{"allowed": false})
			return nil
		}
		return err
	}

	var err error
	if body.State == "started" {
		err = s.deps.Activity.StartPlayback(r.Context(), c.DeviceID, c.ID, body.VideoID)
	} else {
		err = s.deps.Activity.RenewPlayback(r.Context(), c.DeviceID, c.ID, body.VideoID)
	}
	if err != nil {
		return err
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, map[string]bool{"allowed": true})
	return nil
}

func (s *Server) handleActivePlayback(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	children, err := s.deps.Accounts.Children(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}
	names := make(map[uuid.UUID]string, len(children))
	deviceNames := make(map[uuid.UUID]string)
	ids := make([]uuid.UUID, 0, len(children))
	for _, child := range children {
		if p.CanManage(child.ID) {
			ids = append(ids, child.ID)
			names[child.ID] = child.Name
			devices, err := s.deps.Accounts.Devices(r.Context(), child.ID)
			if err != nil {
				return err
			}
			for _, device := range devices {
				deviceNames[device.ID] = device.Name
			}
		}
	}

	wantedCursor := r.URL.Query().Get("cursor")
	deadline := time.NewTimer(playbackLongPoll)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		page, err := s.playbackSnapshot(r.Context(), ids, names, deviceNames)
		if err != nil {
			return err
		}
		if wantedCursor == "" || page.Cursor != wantedCursor {
			writeJSON(w, s.deps.Logger, http.StatusOK, page)
			return nil
		}

		select {
		case <-r.Context().Done():
			return nil
		case <-deadline.C:
			writeJSON(w, s.deps.Logger, http.StatusOK, page)
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Server) playbackSnapshot(ctx context.Context, childIDs []uuid.UUID,
	names map[uuid.UUID]string, deviceNames map[uuid.UUID]string) (playbackPageDTO, error) {
	rows, err := s.deps.Activity.ActivePlaybacks(ctx, childIDs, s.deps.Now().Add(-playbackLeaseWindow))
	if err != nil {
		return playbackPageDTO{}, err
	}

	videoIDs := make([]string, len(rows))
	for i, row := range rows {
		videoIDs[i] = row.VideoID
	}
	videos, err := s.deps.Catalog.VideosByID(ctx, videoIDs)
	if err != nil {
		return playbackPageDTO{}, err
	}
	byID := make(map[string]store.Video, len(videos))
	channelIDs := make([]string, 0, len(videos))
	for _, video := range videos {
		byID[video.ID] = video
		channelIDs = append(channelIDs, video.ChannelID)
	}
	channels, err := s.deps.Catalog.ChannelsByID(ctx, channelIDs)
	if err != nil {
		return playbackPageDTO{}, err
	}

	page := playbackPageDTO{Items: make([]playbackDTO, 0, len(rows))}
	hash := sha256.New()
	for _, row := range rows {
		video, ok := byID[row.VideoID]
		if !ok {
			continue
		}
		_, _ = hash.Write([]byte(row.ChildID.String()))
		_, _ = hash.Write([]byte(row.DeviceID.String()))
		_, _ = hash.Write([]byte(row.VideoID))
		_, _ = hash.Write([]byte(row.StartedAt.UTC().Format(time.RFC3339Nano)))
		page.Items = append(page.Items, playbackDTO{
			ChildID: row.ChildID, ChildName: names[row.ChildID],
			DeviceID: row.DeviceID, DeviceName: deviceNames[row.DeviceID],
			Video:     newVideoDTO(video, channels[video.ChannelID].Title, s.deps.Config.Server.PublicURL),
			StartedAt: row.StartedAt,
		})
	}
	page.Cursor = hex.EncodeToString(hash.Sum(nil))
	return page, nil
}

func (s *Server) handleListVideoBlocks(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	rows, err := s.deps.Rules.BlockedVideos(r.Context(), child.ID)
	if err != nil {
		return err
	}
	out := make([]videoBlockDTO, 0, len(rows))
	for _, row := range rows {
		video, err := s.deps.Catalog.Video(r.Context(), row.VideoID)
		if err != nil {
			return err
		}
		channel, err := s.deps.Catalog.Channel(r.Context(), video.ChannelID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		out = append(out, videoBlockDTO{
			Video:     newVideoDTO(video, channel.Title, s.deps.Config.Server.PublicURL),
			CreatedAt: row.CreatedAt,
		})
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, out)
	return nil
}

func (s *Server) handleBlockVideo(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	videoID := r.PathValue("videoId")
	if _, err := s.deps.Catalog.Video(r.Context(), videoID); err != nil {
		return err
	}
	if err := s.deps.Rules.BlockVideoForChild(r.Context(), child.ID, videoID, p.ID); err != nil {
		return err
	}
	if err := s.deps.Activity.StopChildPlayback(r.Context(), child.ID, videoID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleUnblockVideo(w http.ResponseWriter, r *http.Request, p auth.Parent) error {
	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	if err := s.deps.Rules.UnblockVideoForChild(
		r.Context(), child.ID, r.PathValue("videoId"), p.ID,
	); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
