-- Parent sessions.
--
-- A row per session rather than a token hash on the parent, so signing in on a
-- second device does not silently sign out the first, and so a single device
-- can be revoked without forcing every other one to log in again.
CREATE TABLE parent_session (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id    UUID        NOT NULL REFERENCES parent (id) ON DELETE CASCADE,
    token_hash   TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_parent_session_token_hash ON parent_session (token_hash);
CREATE INDEX idx_parent_session_parent_id ON parent_session (parent_id);
CREATE INDEX idx_parent_session_expires_at ON parent_session (expires_at);
