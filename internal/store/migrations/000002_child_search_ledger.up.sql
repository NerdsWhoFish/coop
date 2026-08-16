-- Per-child daily search accounting.
--
-- child.daily_search_limit needs something to count against, and the family
-- quota ledger is too coarse: it cannot tell which child spent the searches.
-- day is a Pacific-time date string, matching quota_spend, so both ledgers roll
-- over together with Google's quota.
CREATE TABLE child_search (
    child_id   UUID        NOT NULL REFERENCES child (id) ON DELETE CASCADE,
    day        TEXT        NOT NULL,
    count      INTEGER     NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_id, day)
);
