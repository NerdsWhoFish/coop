ALTER TABLE child
    ADD COLUMN web_linking_enabled boolean NOT NULL DEFAULT true;

CREATE TABLE web_device_link (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_token_hash text NOT NULL UNIQUE,
    redemption_token_hash text NOT NULL UNIQUE,
    device_name text NOT NULL,
    child_id uuid REFERENCES child(id) ON DELETE CASCADE,
    approved_by_device_id uuid REFERENCES child_device(id) ON DELETE SET NULL,
    approved_by_parent_id uuid REFERENCES parent(id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    approved_at timestamptz,
    redeemed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT web_device_link_approval_actor CHECK (
        NOT (approved_by_device_id IS NOT NULL AND approved_by_parent_id IS NOT NULL)
    ),
    CONSTRAINT web_device_link_approval_state CHECK (
        (approved_at IS NULL) = (child_id IS NULL)
    ),
    CONSTRAINT web_device_link_redemption_state CHECK (
        redeemed_at IS NULL OR approved_at IS NOT NULL
    )
);

CREATE INDEX web_device_link_expires_at_idx ON web_device_link(expires_at);
CREATE INDEX web_device_link_child_id_idx ON web_device_link(child_id);
