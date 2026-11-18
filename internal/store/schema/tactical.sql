-- The setup of a tactical position (spec §10.1): one per asset, removed on
-- promotion to core. Prices are in the asset's currency.
CREATE TABLE tactical_setups (
    asset_id     INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    note_id      INTEGER NOT NULL REFERENCES research_notes (id) ON DELETE RESTRICT,
    entry_price  TEXT    NOT NULL,
    target_price TEXT    NOT NULL,
    stop_price   TEXT    NOT NULL,
    expires_on   TEXT    NOT NULL,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
