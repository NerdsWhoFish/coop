CREATE TABLE channel_weight (
    child_id UUID NOT NULL REFERENCES child(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    weight SMALLINT NOT NULL CHECK (weight BETWEEN -2 AND 2),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, channel_id)
);

CREATE INDEX idx_channel_weight_child_id ON channel_weight(child_id);
