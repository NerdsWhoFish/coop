-- What each device is actually running, reported on every authenticated
-- request. Nullable because a client older than this column never sends it,
-- and "unknown" is the answer that matters during a migration.
ALTER TABLE child_device
    ADD COLUMN app_build TEXT,
    ADD COLUMN app_version TEXT;

ALTER TABLE parent_session
    ADD COLUMN app_build TEXT,
    ADD COLUMN app_version TEXT;
