BEGIN;

ALTER TABLE allow_global
    ALTER COLUMN approved_by DROP NOT NULL,
    DROP CONSTRAINT allow_global_approved_by_fkey,
    ADD CONSTRAINT allow_global_approved_by_fkey
        FOREIGN KEY (approved_by) REFERENCES parent (id) ON DELETE SET NULL;

ALTER TABLE allow_child
    ALTER COLUMN approved_by DROP NOT NULL,
    DROP CONSTRAINT allow_child_approved_by_fkey,
    ADD CONSTRAINT allow_child_approved_by_fkey
        FOREIGN KEY (approved_by) REFERENCES parent (id) ON DELETE SET NULL;

ALTER TABLE video_override
    ALTER COLUMN created_by DROP NOT NULL,
    DROP CONSTRAINT video_override_created_by_fkey,
    ADD CONSTRAINT video_override_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES parent (id) ON DELETE SET NULL;

ALTER TABLE video_block
    ALTER COLUMN created_by DROP NOT NULL,
    DROP CONSTRAINT video_block_created_by_fkey,
    ADD CONSTRAINT video_block_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES parent (id) ON DELETE SET NULL;

ALTER TABLE request
    DROP CONSTRAINT request_decided_by_fkey,
    ADD CONSTRAINT request_decided_by_fkey
        FOREIGN KEY (decided_by) REFERENCES parent (id) ON DELETE SET NULL;

COMMIT;
