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
    -- Company identifiers for fundamentals: CNPJ (B3, formatted XX.XXX.XXX/XXXX-XX)
    -- and SEC CIK (US, 10 digits). Empty when unknown.
    cnpj       TEXT NOT NULL DEFAULT '',
    cik        TEXT NOT NULL DEFAULT '',
    -- A BDR can point at the US asset it represents. Rating, thesis and fair
    -- value then come from the underlying; bdr_ratio is BDRs per underlying
    -- share (decimal text, empty when not linked).
    underlying_asset_id INTEGER REFERENCES assets (id),
    bdr_ratio  TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (market, ticker)
);
