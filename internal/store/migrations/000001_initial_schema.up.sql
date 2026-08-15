-- Tenancy -------------------------------------------------------------------

CREATE TABLE family (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT        NOT NULL,
    timezone          TEXT        NOT NULL DEFAULT 'UTC',
    encrypted_api_key BYTEA,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE parent (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id             UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    email                 TEXT        NOT NULL,
    password_hash         TEXT        NOT NULL,
    role                  TEXT        NOT NULL CHECK (role IN ('admin', 'parent')),
    encrypted_totp_secret BYTEA,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_parent_email ON parent (email);
CREATE INDEX idx_parent_family_id ON parent (family_id);

CREATE TABLE child (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id           UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,
    avatar_id           TEXT        NOT NULL DEFAULT '',
    shorts_enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    watch_page_autoplay BOOLEAN     NOT NULL DEFAULT FALSE,
    video_search_tiles  BOOLEAN     NOT NULL DEFAULT TRUE,
    daily_search_limit  INTEGER     NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_child_family_id ON child (family_id);

-- A non-admin parent sees only the children listed here. Admins bypass it.
CREATE TABLE parent_scope (
    parent_id  UUID        NOT NULL REFERENCES parent (id) ON DELETE CASCADE,
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (parent_id, child_id)
);

-- Device pairing ------------------------------------------------------------

CREATE TABLE child_device (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    child_id     UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    token_hash   TEXT        NOT NULL,
    last_seen_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_child_device_token_hash ON child_device (token_hash);
CREATE INDEX idx_child_device_child_id ON child_device (child_id);

CREATE TABLE pairing_code (
    code       TEXT PRIMARY KEY,
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_pairing_code_child_id ON pairing_code (child_id);

-- YouTube metadata cache ----------------------------------------------------

CREATE TABLE channel (
    id                  TEXT PRIMARY KEY,
    title               TEXT        NOT NULL DEFAULT '',
    description         TEXT        NOT NULL DEFAULT '',
    thumbnail_url       TEXT        NOT NULL DEFAULT '',
    banner_url          TEXT        NOT NULL DEFAULT '',
    subscriber_count    BIGINT      NOT NULL DEFAULT 0,
    uploads_playlist_id TEXT        NOT NULL DEFAULT '',
    fetched_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_channel_fetched_at ON channel (fetched_at);

CREATE TABLE video (
    id               TEXT PRIMARY KEY,
    channel_id       TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    title            TEXT        NOT NULL DEFAULT '',
    description      TEXT        NOT NULL DEFAULT '',
    tags             TEXT[]      NOT NULL DEFAULT '{}',
    duration_seconds INTEGER     NOT NULL DEFAULT 0,
    published_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    thumbnail_url    TEXT        NOT NULL DEFAULT '',
    is_short         BOOLEAN     NOT NULL DEFAULT FALSE,
    short_source     TEXT CHECK (short_source IN ('rss', 'duration')),
    live_state       TEXT        NOT NULL DEFAULT 'none'
        CHECK (live_state IN ('none', 'live', 'upcoming', 'archived')),
    made_for_kids    BOOLEAN     NOT NULL DEFAULT FALSE,
    fetched_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_video_published_at ON video (published_at);
CREATE INDEX idx_video_is_short ON video (is_short);
CREATE INDEX idx_video_fetched_at ON video (fetched_at);

-- The feed reads non-live videos for a set of channels newest-first, so it gets
-- a composite index rather than relying on the single-column ones.
CREATE INDEX idx_video_feed ON video (channel_id, live_state, published_at DESC);

-- Allowlists ----------------------------------------------------------------

CREATE TABLE allow_global (
    family_id   UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    channel_id  TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    approved_by UUID        NOT NULL REFERENCES parent (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (family_id, channel_id)
);

CREATE TABLE allow_child (
    child_id    UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    channel_id  TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    approved_by UUID        NOT NULL REFERENCES parent (id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, channel_id)
);

CREATE TABLE deny_child (
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    channel_id TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, channel_id)
);

CREATE TABLE block_channel (
    family_id  UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    channel_id TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    reason     TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (family_id, channel_id)
);

-- Keywords ------------------------------------------------------------------

CREATE TABLE keyword (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id         UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    child_id          UUID REFERENCES child (id) ON DELETE CASCADE,
    term              TEXT        NOT NULL,
    match_title       BOOLEAN     NOT NULL DEFAULT TRUE,
    match_tags        BOOLEAN     NOT NULL DEFAULT TRUE,
    match_description BOOLEAN     NOT NULL DEFAULT FALSE,
    whole_word        BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_keyword_family_id ON keyword (family_id);
CREATE INDEX idx_keyword_child_id ON keyword (child_id);

CREATE TABLE video_override (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id  UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    child_id   UUID REFERENCES child (id) ON DELETE CASCADE,
    video_id   TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    created_by UUID        NOT NULL REFERENCES parent (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_video_override_family_id ON video_override (family_id);
CREATE INDEX idx_video_override_child_id ON video_override (child_id);
CREATE INDEX idx_video_override_video_id ON video_override (video_id);

-- Child activity, all local to Coop and never written to YouTube -------------

CREATE TABLE subscription (
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    channel_id TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, channel_id)
);

CREATE TABLE reaction (
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    video_id   TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('like', 'dislike')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, video_id)
);

CREATE TABLE watch_event (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    child_id            UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    video_id            TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    started_at          TIMESTAMPTZ NOT NULL,
    seconds_watched     INTEGER     NOT NULL DEFAULT 0,
    completion_fraction DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_watch_event_video_id ON watch_event (video_id);

-- The ranker scores a child's recent history, so it reads by child newest-first.
CREATE INDEX idx_watch_event_rank ON watch_event (child_id, started_at DESC);

-- Requests and suppressions -------------------------------------------------

CREATE TABLE request (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    child_id             UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    channel_id           TEXT        NOT NULL REFERENCES channel (id) ON DELETE CASCADE,
    prompted_by_video_id TEXT REFERENCES video (id) ON DELETE SET NULL,
    status               TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'denied')),
    decided_by           UUID REFERENCES parent (id),
    decided_at           TIMESTAMPTZ,
    decision_note        TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_request_channel_id ON request (channel_id);

-- The parent queue reads pending requests for a child, newest first.
CREATE INDEX idx_request_queue ON request (child_id, status, created_at DESC);

-- One open ask per child per channel. Re-asking updates the existing row rather
-- than filling the parent's queue with duplicates.
CREATE UNIQUE INDEX idx_request_one_pending
    ON request (child_id, channel_id)
    WHERE status = 'pending';

CREATE TABLE suppression (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    child_id      UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    video_id      TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    keyword_id    UUID        NOT NULL REFERENCES keyword (id) ON DELETE CASCADE,
    matched_field TEXT        NOT NULL DEFAULT '',
    matched_term  TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_suppression_video_id ON suppression (video_id);
CREATE INDEX idx_suppression_keyword_id ON suppression (keyword_id);

-- The parent's suppressed-videos view reads by child, newest first.
CREATE INDEX idx_suppression_review ON suppression (child_id, created_at DESC);

-- Logging the same suppression on every feed build would grow without bound.
CREATE UNIQUE INDEX idx_suppression_unique ON suppression (child_id, video_id, keyword_id);

-- Quota accounting ----------------------------------------------------------

CREATE TABLE api_cache (
    key        TEXT PRIMARY KEY,
    endpoint   TEXT        NOT NULL,
    response   JSONB,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_api_cache_endpoint ON api_cache (endpoint);
CREATE INDEX idx_api_cache_expires_at ON api_cache (expires_at);

-- day is a Pacific-time date string because that is when Google's quota resets.
CREATE TABLE quota_spend (
    family_id  UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    day        TEXT        NOT NULL,
    purpose    TEXT        NOT NULL CHECK (purpose IN ('feed', 'search', 'backfill')),
    units      INTEGER     NOT NULL DEFAULT 0,
    calls      INTEGER     NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (family_id, day, purpose)
);
