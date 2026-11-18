-- Financial facts per asset (spec §12.2), one row per period and canonical
-- concept. Money, shares and per-share values are decimals as TEXT in the
-- reporting currency (full units, not thousands). period_type 'ttm' holds
-- trailing dividends per share fetched from a price/dividend feed.
CREATE TABLE fundamental_facts (
    asset_id    INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    period_end  TEXT    NOT NULL,
    period_type TEXT    NOT NULL CHECK (period_type IN ('annual', 'quarterly', 'ttm')),
    concept     TEXT    NOT NULL,
    value       TEXT    NOT NULL,
    source      TEXT    NOT NULL,
    -- Filing date (SEC) or document version (CVM).
    filed       TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, period_end, period_type, concept)
);

-- One row per asset: the last refresh attempt and its outcome.
CREATE TABLE fundamental_syncs (
    asset_id   INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    source     TEXT    NOT NULL DEFAULT '',
    fetched_at TEXT    NOT NULL,
    -- Latest period end stored (YYYY-MM-DD), empty when nothing was stored.
    as_of      TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL CHECK (status IN ('ok', 'error')),
    message    TEXT    NOT NULL DEFAULT '',
    -- JSON array of strings (skipped years, approximations).
    notes      TEXT    NOT NULL DEFAULT '[]'
);
