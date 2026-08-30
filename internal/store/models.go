// Package store holds Coop's Postgres models and migrations.
//
// Models here are persistence types. The policy engine and ranker take plain
// domain structs and never import this package, so their rules stay testable
// without a database.
package store

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nerdswhofish/coop/internal/domain"
)

// Family is the top-level tenant. Setup currently provisions one per instance.
type Family struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name     string    `gorm:"not null"`
	Timezone string    `gorm:"not null;default:'UTC'"`

	// EncryptedAPIKey is the family's YouTube key, AES-256-GCM sealed.
	EncryptedAPIKey []byte

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Parent is an adult login.
type Parent struct {
	ID           uuid.UUID         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID     uuid.UUID         `gorm:"type:uuid;not null;index"`
	Email        string            `gorm:"not null;uniqueIndex"`
	PasswordHash string            `gorm:"not null"`
	Role         domain.ParentRole `gorm:"type:text;not null"`

	// EncryptedTOTPSecret is nil until the parent enrolls a second factor.
	EncryptedTOTPSecret []byte
	TOTPLastUsedStep    *int64

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ParentAuthChallenge is the short-lived boundary between password and TOTP
// verification. Only the opaque token hash is persisted.
type ParentAuthChallenge struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ParentID            uuid.UUID `gorm:"type:uuid;not null;index"`
	TokenHash           string    `gorm:"not null;uniqueIndex"`
	Purpose             string    `gorm:"type:text;not null"`
	EncryptedTOTPSecret []byte
	ExpiresAt           time.Time `gorm:"not null;index"`
	Attempts            int16     `gorm:"not null;default:0"`
	UsedAt              *time.Time
	CreatedAt           time.Time
}

// AuthThrottle retains failed authentication state across process restarts.
type AuthThrottle struct {
	KeyHash         string    `gorm:"primaryKey"`
	Action          string    `gorm:"primaryKey"`
	Failures        int       `gorm:"not null;default:0"`
	WindowStartedAt time.Time `gorm:"not null"`
	LockedUntil     *time.Time
	UpdatedAt       time.Time `gorm:"not null"`
}

// AuditEvent is an append-only, actor-attributed record of a policy or
// security mutation. Before and After must contain sanitized JSON only.
type AuditEvent struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	ActorParentID *uuid.UUID `gorm:"type:uuid;index"`
	ChildID       *uuid.UUID `gorm:"type:uuid;index"`
	Action        string     `gorm:"not null"`
	TargetType    string     `gorm:"not null"`
	TargetID      string     `gorm:"not null"`
	Before        []byte     `gorm:"type:jsonb;not null;default:'{}'"`
	After         []byte     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt     time.Time
}

// ParentSession is one signed-in parent device. A row per session so a second
// device does not sign out the first, and one can be revoked alone.
type ParentSession struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ParentID   uuid.UUID `gorm:"type:uuid;not null;index"`
	TokenHash  string    `gorm:"not null;uniqueIndex"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	LastSeenAt *time.Time
	AppBuild   string
	AppVersion string
	CreatedAt  time.Time
}

// ParentScope grants a non-admin parent access to one child.
type ParentScope struct {
	ParentID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChildID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time
}

// ParentInvitation is a single-use credential for creating an adult login.
// Only the token hash is persisted; the plain token is returned once.
type ParentInvitation struct {
	ID        uuid.UUID         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID  uuid.UUID         `gorm:"type:uuid;not null;index"`
	Email     string            `gorm:"not null"`
	Role      domain.ParentRole `gorm:"type:text;not null"`
	TokenHash string            `gorm:"not null;uniqueIndex"`
	CreatedBy uuid.UUID         `gorm:"type:uuid;not null"`
	ExpiresAt time.Time         `gorm:"not null;index"`
	UsedAt    *time.Time
	CreatedAt time.Time
}

// ParentInvitationScope becomes a parent scope when its invitation is redeemed.
type ParentInvitationScope struct {
	InvitationID uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChildID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt    time.Time
}

// Child is a viewing profile. It holds no credentials and no personal data
// beyond a display name.
type Child struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID uuid.UUID `gorm:"type:uuid;not null;index"`
	Name     string    `gorm:"not null"`
	AvatarID string

	ShortsEnabled           bool `gorm:"not null;default:true"`
	WatchPageAutoplay       bool `gorm:"not null;default:false"`
	VideoSearchTiles        bool `gorm:"not null;default:true"`
	ChannelDiscoveryEnabled bool `gorm:"not null;default:false"`
	WebLinkingEnabled       bool `gorm:"not null;default:true"`
	DailySearchLimit        int  `gorm:"not null;default:0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChildDevice is one paired device holding a scoped, revocable token.
type ChildDevice struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ChildID         uuid.UUID `gorm:"type:uuid;not null;index"`
	Name            string    `gorm:"not null"`
	TokenHash       string    `gorm:"not null;uniqueIndex"`
	AllowSelfUnpair bool      `gorm:"not null;default:false"`
	LastSeenAt      *time.Time
	AppBuild        string
	AppVersion      string
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

// PairingCode is a single-use, expiring code that binds a device to a child.
type PairingCode struct {
	Code      string    `gorm:"primaryKey"`
	ChildID   uuid.UUID `gorm:"type:uuid;not null;index"`
	ExpiresAt time.Time `gorm:"not null"`
	UsedAt    *time.Time
	CreatedAt time.Time
}

// WebDeviceLink is a short-lived two-secret handoff from a browser to an
// already trusted app. Neither plain secret is persisted.
type WebDeviceLink struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ApprovalTokenHash   string     `gorm:"not null;uniqueIndex"`
	RedemptionTokenHash string     `gorm:"not null;uniqueIndex"`
	DeviceName          string     `gorm:"not null"`
	ChildID             *uuid.UUID `gorm:"type:uuid;index"`
	ApprovedByDeviceID  *uuid.UUID `gorm:"type:uuid"`
	ApprovedByParentID  *uuid.UUID `gorm:"type:uuid"`
	ExpiresAt           time.Time  `gorm:"not null;index"`
	ApprovedAt          *time.Time
	RedeemedAt          *time.Time
	CreatedAt           time.Time
}

// Channel caches YouTube channel metadata. Keyed by YouTube's own ID.
type Channel struct {
	ID              string `gorm:"primaryKey"`
	Title           string
	Description     string
	ThumbnailURL    string
	BannerURL       string
	SubscriberCount int64

	// UploadsPlaylistID is derived from ID, never fetched.
	UploadsPlaylistID string

	FetchedAt        time.Time  `gorm:"index"`
	UploadsFetchedAt *time.Time `gorm:"index"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Video caches YouTube video metadata. Keyed by YouTube's own ID.
type Video struct {
	ID          string `gorm:"primaryKey"`
	ChannelID   string `gorm:"not null;index"`
	Title       string
	Description string
	Tags        pq.StringArray `gorm:"type:text[]"`

	DurationSeconds int
	PublishedAt     time.Time `gorm:"index"`
	ThumbnailURL    string

	IsShort     bool               `gorm:"not null;default:false;index"`
	ShortSource domain.ShortSource `gorm:"type:text"`

	LiveState   domain.LiveState `gorm:"type:text;not null;default:'none';index"`
	MadeForKids bool             `gorm:"not null;default:false"`
	Embeddable  bool             `gorm:"not null;default:true"`

	FetchedAt time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AllowGlobal approves a channel for every child in a family.
type AllowGlobal struct {
	FamilyID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID  string    `gorm:"primaryKey"`
	ApprovedBy uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt  time.Time
}

// AllowChild approves a channel for one child.
type AllowChild struct {
	ChildID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID  string    `gorm:"primaryKey"`
	ApprovedBy uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt  time.Time
}

// DenyChild subtracts a globally approved channel from one child, so an
// age split does not require abandoning the global list.
type DenyChild struct {
	ChildID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID string    `gorm:"primaryKey"`
	CreatedAt time.Time
}

// BlockChannel hides a channel family-wide. Blocked channels are invisible
// rather than locked: a child gets no signal the channel exists.
type BlockChannel struct {
	FamilyID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID string    `gorm:"primaryKey"`
	Reason    string
	CreatedAt time.Time
}

// Keyword suppresses individual videos inside otherwise-allowed channels.
// A nil ChildID makes it family-wide; per-child keywords add to those.
type Keyword struct {
	ID       uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID uuid.UUID  `gorm:"type:uuid;not null;index"`
	ChildID  *uuid.UUID `gorm:"type:uuid;index"`
	Term     string     `gorm:"not null"`

	MatchTitle bool `gorm:"not null;default:true"`
	MatchTags  bool `gorm:"not null;default:true"`
	// MatchDescription defaults off: descriptions carry sponsor copy and
	// boilerplate, so matching them false-positives hard.
	MatchDescription bool `gorm:"not null;default:false"`
	WholeWord        bool `gorm:"not null;default:true"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// VideoOverride re-allows one video a keyword suppressed. A nil ChildID
// applies family-wide.
type VideoOverride struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FamilyID  uuid.UUID  `gorm:"type:uuid;not null;index"`
	ChildID   *uuid.UUID `gorm:"type:uuid;index"`
	VideoID   string     `gorm:"not null;index"`
	CreatedBy uuid.UUID  `gorm:"type:uuid;not null"`
	CreatedAt time.Time
}

// VideoBlock hides one video from one child even when its channel is allowed.
type VideoBlock struct {
	ChildID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	VideoID   string    `gorm:"primaryKey"`
	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt time.Time
}

// Subscription is local to Coop and never written to YouTube.
type Subscription struct {
	ChildID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID string    `gorm:"primaryKey"`
	CreatedAt time.Time
}

// Reaction is a local like or dislike, never written to YouTube.
type Reaction struct {
	ChildID   uuid.UUID           `gorm:"type:uuid;primaryKey"`
	VideoID   string              `gorm:"primaryKey"`
	Kind      domain.ReactionKind `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// WatchEvent records viewing for the ranker. Completion matters more than
// starts, so abandoned videos score themselves down.
type WatchEvent struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ChildID            uuid.UUID `gorm:"type:uuid;not null;index"`
	VideoID            string    `gorm:"not null;index"`
	StartedAt          time.Time `gorm:"not null;index"`
	SecondsWatched     int       `gorm:"not null;default:0"`
	CompletionFraction float64   `gorm:"not null;default:0"`
	CreatedAt          time.Time
}

// PlaybackSession is a renewable lease describing what a child is watching.
// Active sessions older than the lease window are treated as stopped.
type PlaybackSession struct {
	DeviceID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChildID   uuid.UUID `gorm:"type:uuid;not null;index"`
	VideoID   string    `gorm:"not null;index"`
	StartedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null;index"`
	Active    bool      `gorm:"not null;default:true;index"`
}

// ChannelWeight overrides the family's soft preference for one child's feed.
type ChannelWeight struct {
	ChildID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID string    `gorm:"primaryKey"`
	Weight    int       `gorm:"not null"`
	UpdatedAt time.Time
}

// FamilyChannelWeight is inherited unless a child has an explicit override.
type FamilyChannelWeight struct {
	FamilyID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ChannelID string    `gorm:"primaryKey"`
	Weight    int       `gorm:"not null"`
	UpdatedAt time.Time
}

// Request is a child asking for a channel they cannot yet watch.
type Request struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ChildID   uuid.UUID `gorm:"type:uuid;not null;index"`
	ChannelID string    `gorm:"not null;index"`

	// PromptedByVideoID is what the child was looking at when they asked,
	// which gives the reviewing parent context.
	PromptedByVideoID *string

	Status       domain.RequestStatus `gorm:"type:text;not null;default:'pending';index"`
	DecidedBy    *uuid.UUID           `gorm:"type:uuid"`
	DecidedAt    *time.Time
	DecisionNote string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Suppression logs a keyword hiding a video, so the parent app can show what
// was filtered and offer a one-tap override.
type Suppression struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ChildID      uuid.UUID `gorm:"type:uuid;not null;index"`
	VideoID      string    `gorm:"not null;index"`
	KeywordID    uuid.UUID `gorm:"type:uuid;not null;index"`
	MatchedField string
	MatchedTerm  string
	CreatedAt    time.Time `gorm:"index"`
}

// APICache stores raw YouTube responses. It lives in Postgres rather than
// memory so a crash loop cannot re-spend the daily allocation on every restart.
type APICache struct {
	Key       string    `gorm:"primaryKey"`
	Endpoint  string    `gorm:"not null;index"`
	Response  []byte    `gorm:"type:bytea;not null"`
	FetchedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

// ChildSearch is the per-child daily search count that Child.DailySearchLimit
// is enforced against. Day matches QuotaSpend so both roll over together.
type ChildSearch struct {
	ChildID uuid.UUID `gorm:"type:uuid;primaryKey"`
	Day     string    `gorm:"primaryKey"`
	Count   int       `gorm:"not null;default:0"`

	UpdatedAt time.Time
}

// PushToken is one APNs registration. Child tokens ride their device row so
// unpairing revokes them through the cascade; parent tokens are removed at
// sign-out and pruned when Apple reports them gone.
type PushToken struct {
	Token    string              `gorm:"primaryKey"`
	FamilyID uuid.UUID           `gorm:"type:uuid;not null"`
	Audience domain.PushAudience `gorm:"type:text;not null"`
	ParentID *uuid.UUID          `gorm:"type:uuid;index"`
	ChildID  *uuid.UUID          `gorm:"type:uuid;index"`
	DeviceID *uuid.UUID          `gorm:"type:uuid"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// QuotaSpend is the daily ledger behind the circuit breaker. Day is stored in
// Pacific time because that is when Google's quota resets.
type QuotaSpend struct {
	FamilyID uuid.UUID           `gorm:"type:uuid;primaryKey"`
	Day      string              `gorm:"primaryKey"`
	Purpose  domain.QuotaPurpose `gorm:"type:text;primaryKey"`
	Units    int                 `gorm:"not null;default:0"`
	Calls    int                 `gorm:"not null;default:0"`

	UpdatedAt time.Time
}

// AllModels is the canonical list, used by tests to assert that every model has
// a matching migration.
func AllModels() []any {
	return []any{
		&Family{}, &Parent{}, &ParentSession{}, &ParentScope{},
		&ParentAuthChallenge{}, &AuthThrottle{}, &AuditEvent{},
		&ParentInvitation{}, &ParentInvitationScope{},
		&Child{}, &ChildDevice{}, &PairingCode{},
		&Channel{}, &Video{},
		&AllowGlobal{}, &AllowChild{}, &DenyChild{}, &BlockChannel{},
		&Keyword{}, &VideoOverride{}, &VideoBlock{},
		&Subscription{}, &Reaction{}, &WatchEvent{}, &PlaybackSession{},
		&ChannelWeight{}, &FamilyChannelWeight{},
		&Request{}, &Suppression{},
		&APICache{}, &QuotaSpend{}, &ChildSearch{}, &PushToken{},
	}
}
