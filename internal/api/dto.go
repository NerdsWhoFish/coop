package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nerdswhofish/coop/internal/domain"
	"github.com/nerdswhofish/coop/internal/store"
	"github.com/nerdswhofish/coop/internal/youtube"
)

// The shapes here mirror api/openapi.yaml. Responses are built from explicit
// DTOs rather than serialised store rows, so a new column cannot leak to a
// client by accident.

type parentDTO struct {
	ID             uuid.UUID   `json:"id"`
	Email          string      `json:"email"`
	Role           string      `json:"role"`
	TOTPEnrolled   bool        `json:"totpEnrolled"`
	ScopedChildIDs []uuid.UUID `json:"scopedChildIds,omitempty"`
}

func newParentDTO(p store.Parent, scoped []uuid.UUID) parentDTO {
	return parentDTO{
		ID:             p.ID,
		Email:          p.Email,
		Role:           string(p.Role),
		TOTPEnrolled:   len(p.EncryptedTOTPSecret) > 0,
		ScopedChildIDs: scoped,
	}
}

type familyDTO struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Timezone         string    `json:"timezone"`
	APIKeyConfigured bool      `json:"apiKeyConfigured"`
}

type invitationDTO struct {
	Code      string      `json:"code"`
	Email     string      `json:"email"`
	Role      string      `json:"role"`
	ChildIDs  []uuid.UUID `json:"childIds"`
	ExpiresAt time.Time   `json:"expiresAt"`
}

func newFamilyDTO(f store.Family) familyDTO {
	return familyDTO{
		ID:               f.ID,
		Name:             f.Name,
		Timezone:         f.Timezone,
		APIKeyConfigured: len(f.EncryptedAPIKey) > 0,
	}
}

type childDTO struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	AvatarID          string    `json:"avatarId"`
	ShortsEnabled     bool      `json:"shortsEnabled"`
	WatchPageAutoplay bool      `json:"watchPageAutoplay"`
	VideoSearchTiles  bool      `json:"videoSearchTiles"`
	DailySearchLimit  int       `json:"dailySearchLimit"`
	DeviceCount       int       `json:"deviceCount"`
	PendingRequests   int       `json:"pendingRequestCount"`
}

func newChildDTO(c store.Child, devices, pending int) childDTO {
	return childDTO{
		ID:                c.ID,
		Name:              c.Name,
		AvatarID:          c.AvatarID,
		ShortsEnabled:     c.ShortsEnabled,
		WatchPageAutoplay: c.WatchPageAutoplay,
		VideoSearchTiles:  c.VideoSearchTiles,
		DailySearchLimit:  c.DailySearchLimit,
		DeviceCount:       devices,
		PendingRequests:   pending,
	}
}

// childProfileDTO is what the child app sees about itself. Deliberately
// narrower than childDTO: a child has no business knowing their request count
// or how many devices a parent has paired.
type childProfileDTO struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	AvatarID          string    `json:"avatarId"`
	ShortsEnabled     bool      `json:"shortsEnabled"`
	WatchPageAutoplay bool      `json:"watchPageAutoplay"`
	VideoSearchTiles  bool      `json:"videoSearchTiles"`
}

func newChildProfileDTO(c store.Child) childProfileDTO {
	return childProfileDTO{
		ID:                c.ID,
		Name:              c.Name,
		AvatarID:          c.AvatarID,
		ShortsEnabled:     c.ShortsEnabled,
		WatchPageAutoplay: c.WatchPageAutoplay,
		VideoSearchTiles:  c.VideoSearchTiles,
	}
}

type channelDTO struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Description     string     `json:"description,omitempty"`
	ThumbnailURL    string     `json:"thumbnailUrl,omitempty"`
	BannerURL       string     `json:"bannerUrl,omitempty"`
	SubscriberCount int64      `json:"subscriberCount,omitempty"`
	State           string     `json:"state,omitempty"`
	PendingRequest  bool       `json:"pendingRequest,omitempty"`
	YouTubeURL      string     `json:"youtubeUrl,omitempty"`
	Source          string     `json:"source,omitempty"`
	DeniedForChild  bool       `json:"deniedForChild,omitempty"`
	ApprovedAt      *time.Time `json:"approvedAt,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

func newChannelDTO(c store.Channel) channelDTO {
	return channelDTO{
		ID:              c.ID,
		Title:           c.Title,
		Description:     c.Description,
		ThumbnailURL:    c.ThumbnailURL,
		BannerURL:       c.BannerURL,
		SubscriberCount: c.SubscriberCount,
	}
}

type videoDTO struct {
	ID              string    `json:"id"`
	ChannelID       string    `json:"channelId"`
	ChannelTitle    string    `json:"channelTitle,omitempty"`
	Title           string    `json:"title"`
	ThumbnailURL    string    `json:"thumbnailUrl,omitempty"`
	DurationSeconds int       `json:"durationSeconds"`
	PublishedAt     time.Time `json:"publishedAt"`
	IsShort         bool      `json:"isShort"`
	Locked          bool      `json:"locked,omitempty"`
}

// newVideoDTO points the thumbnail at this instance rather than at Google, so
// a child device makes no image request to a YouTube host.
func newVideoDTO(v store.Video, channelTitle, publicURL string) videoDTO {
	return videoDTO{
		ID:              v.ID,
		ChannelID:       v.ChannelID,
		ChannelTitle:    channelTitle,
		Title:           v.Title,
		ThumbnailURL:    publicURL + "/api/v1/thumb/" + v.ID,
		DurationSeconds: v.DurationSeconds,
		PublishedAt:     v.PublishedAt,
		IsShort:         v.IsShort,
	}
}

type videoPageDTO struct {
	Items      []videoDTO `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type recommendationDTO struct {
	Video      videoDTO `json:"video"`
	Score      float64  `json:"score"`
	Reason     string   `json:"reason"`
	ReasonKind string   `json:"reasonKind"`
}

type recommendationPageDTO struct {
	Items      []recommendationDTO `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type channelWeightDTO struct {
	ChannelID string `json:"channelId"`
	Weight    int    `json:"weight"`
}

type watchPageDTO struct {
	Video    videoDTO `json:"video"`
	EmbedURL string   `json:"embedUrl"`
	Autoplay bool     `json:"autoplay"`
	Reaction string   `json:"reaction,omitempty"`
	ShareURL string   `json:"shareUrl"`
}

type requestDTO struct {
	ID            uuid.UUID  `json:"id"`
	ChildID       uuid.UUID  `json:"childId"`
	ChildName     string     `json:"childName,omitempty"`
	Channel       channelDTO `json:"channel"`
	Status        string     `json:"status"`
	Note          string     `json:"note,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	DecidedAt     *time.Time `json:"decidedAt,omitempty"`
	DecidedByName string     `json:"decidedByName,omitempty"`
}

type suppressionDTO struct {
	ID           uuid.UUID `json:"id"`
	Video        videoDTO  `json:"video"`
	Term         string    `json:"term"`
	MatchedField string    `json:"matchedField"`
	CreatedAt    time.Time `json:"createdAt"`
}

type keywordDTO struct {
	ID               uuid.UUID  `json:"id"`
	ChildID          *uuid.UUID `json:"childId,omitempty"`
	Term             string     `json:"term"`
	MatchTitle       bool       `json:"matchTitle"`
	MatchTags        bool       `json:"matchTags"`
	MatchDescription bool       `json:"matchDescription"`
	WholeWord        bool       `json:"wholeWord"`
}

func newKeywordDTO(k store.Keyword) keywordDTO {
	return keywordDTO{
		ID:               k.ID,
		ChildID:          k.ChildID,
		Term:             k.Term,
		MatchTitle:       k.MatchTitle,
		MatchTags:        k.MatchTags,
		MatchDescription: k.MatchDescription,
		WholeWord:        k.WholeWord,
	}
}

type deviceDTO struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

func newDeviceDTO(d store.ChildDevice) deviceDTO {
	return deviceDTO{
		ID:         d.ID,
		Name:       d.Name,
		CreatedAt:  d.CreatedAt,
		LastSeenAt: d.LastSeenAt,
	}
}

type quotaDTO struct {
	Purpose  string    `json:"purpose"`
	Used     int       `json:"used"`
	Budget   int       `json:"budget"`
	ResetsAt time.Time `json:"resetsAt"`
}

type sessionDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Parent    parentDTO `json:"parent"`
}

type authEnrollmentDTO struct {
	Secret          string `json:"secret"`
	ProvisioningURL string `json:"provisioningUrl"`
}

type authChallengeDTO struct {
	Challenge string             `json:"challenge"`
	ExpiresAt time.Time          `json:"expiresAt"`
	Method    string             `json:"method"`
	Enroll    *authEnrollmentDTO `json:"enrollment,omitempty"`
}

type auditEventDTO struct {
	ID            uuid.UUID       `json:"id"`
	ActorParentID *uuid.UUID      `json:"actorParentId,omitempty"`
	ChildID       *uuid.UUID      `json:"childId,omitempty"`
	Action        string          `json:"action"`
	TargetType    string          `json:"targetType"`
	TargetID      string          `json:"targetId"`
	Before        json.RawMessage `json:"before"`
	After         json.RawMessage `json:"after"`
	CreatedAt     time.Time       `json:"createdAt"`
}

func newAuditEventDTO(event store.AuditEvent) auditEventDTO {
	return auditEventDTO{
		ID: event.ID, ActorParentID: event.ActorParentID, ChildID: event.ChildID,
		Action: event.Action, TargetType: event.TargetType, TargetID: event.TargetID,
		Before: event.Before, After: event.After, CreatedAt: event.CreatedAt,
	}
}

type pairingCodeDTO struct {
	Code       string    `json:"code"`
	ExpiresAt  time.Time `json:"expiresAt"`
	PairingURL string    `json:"pairingUrl"`
}

// channelState renders a policy state for the wire.
func channelState(s domain.ChannelState) string { return string(s) }

// youtubeChannelURL is the deep link the parent app opens to review content.
func youtubeChannelURL(channelID string) string { return youtube.ChannelURL(channelID) }
