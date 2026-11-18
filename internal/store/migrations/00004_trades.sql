-- +goose Up
CREATE TABLE trades (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id   INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    trade_date TEXT    NOT NULL,
    side       TEXT    NOT NULL CHECK (side IN ('buy', 'sell')),
    quantity   TEXT    NOT NULL,
    price      TEXT    NOT NULL,
    fees       TEXT    NOT NULL DEFAULT '0',
    currency   TEXT    NOT NULL CHECK (currency IN ('BRL', 'USD')),
    -- BRL per 1 unit of trade currency on the trade date ('1' for BRL trades).
    fx_rate    TEXT    NOT NULL DEFAULT '1',
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX trades_asset_date ON trades (asset_id, trade_date, id);

-- +goose Down
DROP TABLE trades;
