DELETE FROM playback_session;
DROP INDEX idx_playback_session_child_id;
ALTER TABLE playback_session DROP CONSTRAINT playback_session_pkey;
ALTER TABLE playback_session ADD PRIMARY KEY (child_id);
ALTER TABLE playback_session DROP COLUMN device_id;

ALTER TABLE child_device DROP COLUMN allow_self_unpair;
