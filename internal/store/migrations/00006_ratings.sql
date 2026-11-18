-- +goose Up
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
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX ratings_asset_date ON ratings (asset_id, rated_on, id);

-- +goose Down
DROP TABLE ratings;
