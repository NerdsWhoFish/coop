package api

import (
	"cmp"
	"net/http"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/nerdswhofish/coop/internal/auth"
)

func (s *Server) handleChildRecommendations(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	page, err := s.deps.Feed.Recommendations(r.Context(), p.FamilyID, child.ID,
		queryInt(r, "limit", 30), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}

	channelIDs := make([]string, 0, len(page.Recommendations))
	seen := make(map[string]bool, len(page.Recommendations))
	for _, recommendation := range page.Recommendations {
		if !seen[recommendation.Video.ChannelID] {
			channelIDs = append(channelIDs, recommendation.Video.ChannelID)
			seen[recommendation.Video.ChannelID] = true
		}
	}
	channels, err := s.deps.Catalog.ChannelsByID(r.Context(), channelIDs)
	if err != nil {
		return err
	}

	items := make([]recommendationDTO, 0, len(page.Recommendations))
	for _, recommendation := range page.Recommendations {
		items = append(items, recommendationDTO{
			Video: newVideoDTO(
				recommendation.Video,
				channels[recommendation.Video.ChannelID].Title,
				s.deps.Config.Server.PublicURL,
			),
			Score:      recommendation.Score,
			Reason:     recommendation.Reason,
			ReasonKind: string(recommendation.ReasonKind),
		})
	}
	writeJSON(w, s.deps.Logger, http.StatusOK, recommendationPageDTO{
		Items: items, NextCursor: page.NextCursor,
	})
	return nil
}

func (s *Server) handleChildChannelWeights(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	weights, err := s.deps.Feed.ChannelWeights(r.Context(), child.ID)
	if err != nil {
		return err
	}
	writeChannelWeights(w, s, weights)
	return nil
}

func (s *Server) handleFamilyChannelWeights(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	if err := p.RequireAdmin(); err != nil {
		return err
	}
	weights, err := s.deps.Feed.FamilyChannelWeights(r.Context(), p.FamilyID)
	if err != nil {
		return err
	}
	writeChannelWeights(w, s, weights)
	return nil
}

func writeChannelWeights(w http.ResponseWriter, s *Server, weights map[string]int) {
	out := make([]channelWeightDTO, 0, len(weights))
	for channelID, weight := range weights {
		out = append(out, channelWeightDTO{ChannelID: channelID, Weight: weight})
	}
	slices.SortFunc(out, func(a, b channelWeightDTO) int {
		return cmp.Compare(a.ChannelID, b.ChannelID)
	})
	writeJSON(w, s.deps.Logger, http.StatusOK, out)
}

func (s *Server) handleSetChildChannelWeight(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	child, err := s.childInScope(r, p)
	if err != nil {
		return err
	}
	var body struct {
		Weight *int `json:"weight"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Weight == nil {
		return badRequest("weight is required")
	}
	if *body.Weight < -2 || *body.Weight > 2 {
		return badRequest("weight must be between -2 and 2")
	}
	if err := s.deps.Feed.SetChannelWeight(
		r.Context(), p.FamilyID, child.ID, r.PathValue("channelId"), *body.Weight, p.ID,
	); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleSetFamilyChannelWeight(w http.ResponseWriter, r *http.Request,
	p auth.Parent) error {

	if err := p.RequireAdmin(); err != nil {
		return err
	}
	var body struct {
		Weight *int `json:"weight"`
	}
	if err := decodeJSON(r, &body); err != nil {
		return err
	}
	if body.Weight == nil {
		return badRequest("weight is required")
	}
	if *body.Weight < -2 || *body.Weight > 2 {
		return badRequest("weight must be between -2 and 2")
	}
	if err := s.deps.Feed.SetFamilyChannelWeight(
		r.Context(), p.FamilyID, r.PathValue("channelId"), *body.Weight, p.ID,
	); err != nil {
		return err
	}
	trace.SpanFromContext(r.Context()).AddEvent(
		"coop.recommendation.household_weight.changed",
		trace.WithAttributes(attribute.Int("coop.recommendation.weight", *body.Weight)),
	)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
