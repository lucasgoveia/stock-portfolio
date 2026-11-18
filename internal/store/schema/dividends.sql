-- Dividends and JCP (spec §8.5). Amounts are in the asset's currency;
-- fx_rate converts to BRL at the payment date (1 for BRL). Detected rows
-- come from data sources and wait for confirmation; confirming credits net
-- to a cash instrument through a ledger movement.
CREATE TABLE dividends (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id         INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind             TEXT    NOT NULL CHECK (kind IN ('dividend', 'jcp')),
    -- Last day holding the shares gives the right (data-com).
    record_date      TEXT    NOT NULL,
    pay_date         TEXT,
    quantity         TEXT    NOT NULL,
    per_share        TEXT    NOT NULL,
    gross            TEXT    NOT NULL,
    withholding      TEXT    NOT NULL DEFAULT '0',
    net              TEXT    NOT NULL,
    currency         TEXT    NOT NULL CHECK (currency IN ('BRL', 'USD')),
    fx_rate          TEXT    NOT NULL DEFAULT '1',
    status           TEXT    NOT NULL DEFAULT 'confirmed' CHECK (status IN ('detected', 'confirmed', 'rejected')),
    source           TEXT    NOT NULL DEFAULT 'manual',
    movement_id      INTEGER REFERENCES cash_movements (id) ON DELETE SET NULL,
    contribution_id  INTEGER REFERENCES contributions (id) ON DELETE SET NULL,
    created_at       TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- A data source proposes each payment once.
CREATE UNIQUE INDEX dividends_detected ON dividends (asset_id, kind, record_date, source) WHERE source <> 'manual';
CREATE INDEX dividends_asset ON dividends (asset_id, record_date);
