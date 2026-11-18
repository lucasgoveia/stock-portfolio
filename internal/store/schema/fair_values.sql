CREATE TABLE fair_values (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id        INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    method          TEXT    NOT NULL
                    CHECK (method IN ('scenario_dcf', 'bazin', 'graham', 'manual', 'target_price')),
    -- Decimal as TEXT, per share, in currency.
    value_per_share TEXT    NOT NULL,
    currency        TEXT    NOT NULL CHECK (currency IN ('BRL', 'USD')),
    -- Date the estimate refers to (YYYY-MM-DD); drives staleness.
    as_of           TEXT    NOT NULL,
    -- Method inputs and computed outputs (JSON object).
    inputs          TEXT    NOT NULL DEFAULT '{}',
    -- JSON array of strings (links or citations).
    sources         TEXT    NOT NULL DEFAULT '[]',
    note            TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL
);
CREATE INDEX fair_values_asset ON fair_values (asset_id, as_of, id);
