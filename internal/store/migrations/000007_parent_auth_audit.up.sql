ALTER TABLE parent
    ADD COLUMN totp_last_used_step BIGINT;

CREATE TABLE parent_auth_challenge (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id             UUID        NOT NULL REFERENCES parent (id) ON DELETE CASCADE,
    token_hash            TEXT        NOT NULL,
    purpose               TEXT        NOT NULL CHECK (purpose IN ('login', 'enroll')),
    encrypted_totp_secret BYTEA,
    expires_at            TIMESTAMPTZ NOT NULL,
    attempts              SMALLINT    NOT NULL DEFAULT 0,
    used_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_parent_auth_challenge_token_hash
    ON parent_auth_challenge (token_hash);
CREATE INDEX idx_parent_auth_challenge_expires_at
    ON parent_auth_challenge (expires_at);

CREATE TABLE auth_throttle (
    key_hash          TEXT        NOT NULL,
    action            TEXT        NOT NULL,
    failures          INTEGER     NOT NULL DEFAULT 0,
    window_started_at TIMESTAMPTZ NOT NULL,
    locked_until      TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (key_hash, action)
);
CREATE INDEX idx_auth_throttle_expiry
    ON auth_throttle (locked_until, window_started_at);

CREATE TABLE audit_event (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id       UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    actor_parent_id UUID REFERENCES parent (id) ON DELETE SET NULL,
    child_id        UUID REFERENCES child (id) ON DELETE SET NULL,
    action          TEXT        NOT NULL,
    target_type     TEXT        NOT NULL,
    target_id       TEXT        NOT NULL,
    before          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    after           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_event_family_created
    ON audit_event (family_id, created_at DESC);
CREATE INDEX idx_audit_event_child_created
    ON audit_event (child_id, created_at DESC);
