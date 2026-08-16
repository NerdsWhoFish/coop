ALTER TABLE channel ADD COLUMN uploads_fetched_at TIMESTAMPTZ;

CREATE INDEX idx_channel_uploads_fetched_at ON channel (uploads_fetched_at);
