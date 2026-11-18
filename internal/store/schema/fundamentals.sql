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

-- Quarterly income statement figures (spec §12.4), one row per fiscal quarter.
-- Three-month values as decimals in the reporting currency; an empty string
-- means the figure is not available. A Q4 is derived (annual minus the Q3 year
-- to date) and marked derived = 1.
CREATE TABLE fundamental_quarters (
    asset_id       INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    period_end     TEXT    NOT NULL,
    fiscal_quarter INTEGER NOT NULL CHECK (fiscal_quarter BETWEEN 1 AND 4),
    revenue        TEXT    NOT NULL DEFAULT '',
    net_income     TEXT    NOT NULL DEFAULT '',
    -- Diluted EPS (SEC) or net income over period-end shares (CVM).
    eps            TEXT    NOT NULL DEFAULT '',
    shares         TEXT    NOT NULL DEFAULT '',
    derived        INTEGER NOT NULL DEFAULT 0 CHECK (derived IN (0, 1)),
    source         TEXT    NOT NULL,
    -- Filing date, or the document version when the date is unknown.
    filed          TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, period_end)
);
