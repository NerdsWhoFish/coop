DROP TABLE audit_event;
DROP TABLE auth_throttle;
DROP TABLE parent_auth_challenge;
ALTER TABLE parent DROP COLUMN totp_last_used_step;
