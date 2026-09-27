BEGIN;

-- Deleted actors cannot be reconstructed. Keep attribution nullable so rollback
-- preserves their policy and history instead of deleting or reassigning it.
ALTER TABLE allow_global
    DROP CONSTRAINT allow_global_approved_by_fkey,
    ADD CONSTRAINT allow_global_approved_by_fkey
        FOREIGN KEY (approved_by) REFERENCES parent (id);

ALTER TABLE allow_child
    DROP CONSTRAINT allow_child_approved_by_fkey,
    ADD CONSTRAINT allow_child_approved_by_fkey
        FOREIGN KEY (approved_by) REFERENCES parent (id);

ALTER TABLE video_override
    DROP CONSTRAINT video_override_created_by_fkey,
    ADD CONSTRAINT video_override_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES parent (id);

ALTER TABLE video_block
    DROP CONSTRAINT video_block_created_by_fkey,
    ADD CONSTRAINT video_block_created_by_fkey
        FOREIGN KEY (created_by) REFERENCES parent (id);

ALTER TABLE request
    DROP CONSTRAINT request_decided_by_fkey,
    ADD CONSTRAINT request_decided_by_fkey
        FOREIGN KEY (decided_by) REFERENCES parent (id);

COMMIT;
