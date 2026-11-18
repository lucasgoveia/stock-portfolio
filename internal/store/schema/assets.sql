CREATE TABLE assets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ticker     TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    market     TEXT NOT NULL CHECK (market IN ('B3', 'US')),
    currency   TEXT NOT NULL CHECK (currency IN ('BRL', 'USD')),
    kind       TEXT NOT NULL CHECK (kind IN ('stock', 'bdr', 'etf')),
    sleeve     TEXT NOT NULL CHECK (sleeve IN ('core', 'tactical')),
    status     TEXT NOT NULL DEFAULT 'watchlist' CHECK (status IN ('watchlist', 'holding', 'exited')),
    benchmark  TEXT NOT NULL CHECK (benchmark IN ('IBOV', 'SP500')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (market, ticker)
);
