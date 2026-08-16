DROP INDEX IF EXISTS idx_channel_uploads_fetched_at;

ALTER TABLE channel DROP COLUMN IF EXISTS uploads_fetched_at;
