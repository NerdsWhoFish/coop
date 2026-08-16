CREATE TABLE parent_invitation (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id  UUID        NOT NULL REFERENCES family (id) ON DELETE CASCADE,
    email      TEXT        NOT NULL,
    role       TEXT        NOT NULL CHECK (role IN ('admin', 'parent')),
    token_hash TEXT        NOT NULL UNIQUE,
    created_by UUID        NOT NULL REFERENCES parent (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_parent_invitation_family_id ON parent_invitation (family_id);
CREATE INDEX idx_parent_invitation_expires_at ON parent_invitation (expires_at);

CREATE TABLE parent_invitation_scope (
    invitation_id UUID        NOT NULL REFERENCES parent_invitation (id) ON DELETE CASCADE,
    child_id      UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (invitation_id, child_id)
);
