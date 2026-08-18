CREATE TABLE push_token (
    token text PRIMARY KEY,
    family_id uuid NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    audience text NOT NULL CHECK (audience IN ('parent', 'child')),
    parent_id uuid REFERENCES parent (id) ON DELETE CASCADE,
    child_id uuid REFERENCES child (id) ON DELETE CASCADE,
    device_id uuid REFERENCES child_device (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT push_token_owner CHECK (
        (audience = 'parent' AND parent_id IS NOT NULL AND child_id IS NULL AND device_id IS NULL)
        OR (audience = 'child' AND child_id IS NOT NULL AND device_id IS NOT NULL AND parent_id IS NULL)
    )
);

CREATE INDEX push_token_parent_id_idx ON push_token (parent_id);
CREATE INDEX push_token_child_id_idx ON push_token (child_id);
