DROP TABLE IF EXISTS web_device_link;

ALTER TABLE child
    DROP COLUMN IF EXISTS web_linking_enabled;
