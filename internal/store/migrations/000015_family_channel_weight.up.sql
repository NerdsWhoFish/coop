CREATE TABLE family_channel_weight (
    family_id UUID NOT NULL REFERENCES family(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    weight SMALLINT NOT NULL CHECK (weight BETWEEN -2 AND 2),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (family_id, channel_id)
);

CREATE INDEX idx_family_channel_weight_family_id ON family_channel_weight(family_id);
