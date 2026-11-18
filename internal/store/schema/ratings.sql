-- Append-only: rows are never updated or deleted; the latest (rated_on, id) wins.
CREATE TABLE ratings (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id     INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    rated_on     TEXT    NOT NULL,
    volatility   INTEGER NOT NULL CHECK (volatility BETWEEN 1 AND 5),
    understanding INTEGER NOT NULL CHECK (understanding BETWEEN 1 AND 5),
    moat         INTEGER NOT NULL CHECK (moat BETWEEN 1 AND 5),
    predictability INTEGER NOT NULL CHECK (predictability BETWEEN 1 AND 5),
    horizon      INTEGER NOT NULL CHECK (horizon BETWEEN 1 AND 5),
    reason       TEXT    NOT NULL,
    -- 1 when volatility was filled from beta (auto), 0 when set by hand (override).
    volatility_auto INTEGER NOT NULL DEFAULT 0 CHECK (volatility_auto IN (0, 1)),
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX ratings_asset_date ON ratings (asset_id, rated_on, id);

-- Latest beta per asset (spec 5.2): 2y weekly log returns vs the asset's benchmark.
CREATE TABLE asset_betas (
    asset_id    INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    index_code  TEXT    NOT NULL,
    beta        TEXT    NOT NULL,
    weeks       INTEGER NOT NULL,
    computed_on TEXT    NOT NULL
);
