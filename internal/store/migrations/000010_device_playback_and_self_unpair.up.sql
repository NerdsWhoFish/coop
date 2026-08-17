ALTER TABLE child_device
    ADD COLUMN allow_self_unpair BOOLEAN NOT NULL DEFAULT FALSE;

DELETE FROM playback_session;
ALTER TABLE playback_session DROP CONSTRAINT playback_session_pkey;
ALTER TABLE playback_session
    ADD COLUMN device_id UUID NOT NULL REFERENCES child_device (id) ON DELETE CASCADE;
ALTER TABLE playback_session ADD PRIMARY KEY (device_id);
CREATE INDEX idx_playback_session_child_id ON playback_session (child_id);
