-- Corporate actions (spec §14, §11.1). factor multiplies the shares held
-- before ex_date: 2 for a 2-for-1 split, 0.01 for a 1-for-100 reverse split,
-- 1.1 for a 10% bonificação. Bonus shares enter at cost_per_share (asset
-- currency). Only confirmed rows change positions.
CREATE TABLE corporate_actions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id       INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind           TEXT    NOT NULL CHECK (kind IN ('split', 'reverse_split', 'bonus')),
    -- First trading day without the right; applied before that day's trades.
    ex_date        TEXT    NOT NULL,
    factor         TEXT    NOT NULL,
    cost_per_share TEXT    NOT NULL DEFAULT '0',
    status         TEXT    NOT NULL DEFAULT 'confirmed' CHECK (status IN ('detected', 'confirmed', 'rejected')),
    source         TEXT    NOT NULL DEFAULT 'manual',
    -- Close before / close on ex_date (raw), when known, for the cross-check.
    price_ratio    TEXT,
    note           TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE UNIQUE INDEX corporate_actions_detected ON corporate_actions (asset_id, kind, ex_date, source) WHERE source <> 'manual';
CREATE INDEX corporate_actions_asset ON corporate_actions (asset_id, ex_date);
