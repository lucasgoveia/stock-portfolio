CREATE TABLE cash_instruments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL CHECK (kind IN ('tesouro_selic', 'cdi', 'usd_cash')),
    name       TEXT NOT NULL,
    currency   TEXT NOT NULL CHECK (currency IN ('BRL', 'USD')),
    -- Contracted percentage of CDI (e.g. '100', '110'); only used by kind = 'cdi'.
    pct_cdi    TEXT NOT NULL DEFAULT '100',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE cash_movements (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    instrument_id INTEGER NOT NULL REFERENCES cash_instruments (id) ON DELETE CASCADE,
    move_date     TEXT NOT NULL,
    -- balance = manual balance update: sets the balance to amount on move_date.
    type          TEXT NOT NULL CHECK (type IN ('deposit', 'withdraw', 'yield_adjust', 'balance')),
    amount        TEXT NOT NULL,
    -- Title units; only for tesouro_selic.
    quantity      TEXT NOT NULL DEFAULT '0',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX cash_movements_instrument ON cash_movements (instrument_id, move_date, id);

-- Valuation inputs. The market data job (issue #10) fills these tables.

-- Daily CDI rate in percent per day (BCB SGS 12), by reference date.
CREATE TABLE cdi_rates (
    ref_date TEXT PRIMARY KEY,
    rate_pct TEXT NOT NULL
);

-- Tesouro Selic unit price (PU) by reference date.
CREATE TABLE tesouro_selic_prices (
    ref_date TEXT PRIMARY KEY,
    price    TEXT NOT NULL
);

-- BRL per 1 unit of base currency (pair 'USDBRL') by reference date.
CREATE TABLE fx_rates (
    pair     TEXT NOT NULL,
    ref_date TEXT NOT NULL,
    rate     TEXT NOT NULL,
    PRIMARY KEY (pair, ref_date)
);
