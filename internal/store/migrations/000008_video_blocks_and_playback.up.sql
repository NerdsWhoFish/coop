CREATE TABLE video_block (
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    video_id   TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    created_by UUID        NOT NULL REFERENCES parent (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, video_id)
);
CREATE INDEX idx_video_block_video_id ON video_block (video_id);

CREATE TABLE playback_session (
    child_id   UUID        PRIMARY KEY REFERENCES child (id) ON DELETE CASCADE,
    video_id   TEXT        NOT NULL REFERENCES video (id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    active     BOOLEAN     NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_playback_session_active ON playback_session (active, updated_at DESC);
