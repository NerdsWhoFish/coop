// Package domain holds the value types shared between policy evaluation and
// persistence. It imports nothing, so importing it can never create a cycle.
package domain

// ChannelState is a channel's effective visibility for one child.
type ChannelState string

const (
	// StateBlocked channels are invisible: never listed, never requestable,
	// with no signal to the child that they exist.
	StateBlocked ChannelState = "blocked"
	// StateRequestable channels show their branding and an ask affordance,
	// but serve no videos.
	StateRequestable ChannelState = "requestable"
	// StateAllowed channels behave normally, subject to keyword rules.
	StateAllowed ChannelState = "allowed"
)

// LiveState classifies a video against livestreaming. Archived is separate
// because a finished stream reverts to liveBroadcastContent "none" while
// keeping liveStreamingDetails, so one field lets old streams through.
type LiveState string

const (
	LiveNone     LiveState = "none"
	LiveLive     LiveState = "live"
	LiveUpcoming LiveState = "upcoming"
	LiveArchived LiveState = "archived"
)

// IsLive reports whether the state is any form of stream, which never reaches
// a child regardless of who approved the channel. Enumerated positively so the
// zero value reads as "not live" instead of emptying every feed.
func (s LiveState) IsLive() bool {
	return s == LiveLive || s == LiveUpcoming || s == LiveArchived
}

// ShortSource records how a Shorts classification was decided. The RSS window
// only moves forward, so a duration guess is permanent for that row and repair
// is a manual pass, not a scheduled one.
type ShortSource string

const (
	// ShortSourceRSS is authoritative: YouTube's own canonical URL said so.
	ShortSourceRSS ShortSource = "rss"
	// ShortSourceDuration is a guess, used only for back catalog.
	ShortSourceDuration ShortSource = "duration"
)

// ParentRole controls how much of a family a parent can see and change.
type ParentRole string

const (
	// RoleAdmin manages parents, holds the API key, and sees every child.
	RoleAdmin ParentRole = "admin"
	// RoleParent sees only the children granted by ParentScope.
	RoleParent ParentRole = "parent"
)

// RequestStatus tracks a child's ask through parent review.
type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestDenied   RequestStatus = "denied"
)

// ReactionKind is a child's local like or dislike, never sent to YouTube.
type ReactionKind string

const (
	ReactionLike    ReactionKind = "like"
	ReactionDislike ReactionKind = "dislike"
)

// QuotaPurpose partitions daily spend so backfill cannot starve the feed.
type QuotaPurpose string

const (
	PurposeFeed     QuotaPurpose = "feed"
	PurposeSearch   QuotaPurpose = "search"
	PurposeBackfill QuotaPurpose = "backfill"
)

// MatchField names the video field a keyword matched, so a parent reviewing a
// suppression can see why it fired.
type MatchField string

const (
	FieldTitle       MatchField = "title"
	FieldTags        MatchField = "tags"
	FieldDescription MatchField = "description"
)
