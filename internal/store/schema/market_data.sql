-- Daily closes per asset and source. COTAHIST and Tiingo closes are raw
-- (unadjusted); brapi quotes are the latest price of the day.
CREATE TABLE asset_prices (
    asset_id   INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    ref_date   TEXT    NOT NULL,
    source     TEXT    NOT NULL CHECK (source IN ('cotahist', 'brapi', 'tiingo', 'manual')),
    close      TEXT    NOT NULL,
    -- Adjusted close when the source provides one (Tiingo).
    adj_close  TEXT,
    fetched_at TEXT    NOT NULL,
    PRIMARY KEY (asset_id, ref_date, source)
);

-- Daily index closes ('IBOV', 'SP500').
CREATE TABLE index_points (
    index_code TEXT NOT NULL,
    ref_date   TEXT NOT NULL,
    close      TEXT NOT NULL,
    PRIMARY KEY (index_code, ref_date)
);

-- One row per job execution; steps is a JSON array of step results.
CREATE TABLE job_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    job         TEXT NOT NULL,
    run_date    TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    finished_at TEXT,
    status      TEXT NOT NULL CHECK (status IN ('running', 'ok', 'partial', 'failed')),
    steps       TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX job_runs_job ON job_runs (job, id);

-- Marks that the daily job already tried to load 2 years of history for a
-- subject ('asset:<id>' or 'index:<code>') from a source, so a recently listed
-- asset with a shorter history is not refetched every day.
CREATE TABLE history_backfills (
    subject   TEXT NOT NULL,
    source    TEXT NOT NULL,
    from_date TEXT NOT NULL,
    done_at   TEXT NOT NULL,
    PRIMARY KEY (subject, source)
);

-- Risk-free rates for the CAPM cost of equity, as fractions: 'US' is the
-- effective federal funds rate (FRED DFF), 'B3' the Selic target (BCB SGS 432).
CREATE TABLE risk_free_rates (
    code     TEXT NOT NULL,
    ref_date TEXT NOT NULL,
    rate     TEXT NOT NULL,
    PRIMARY KEY (code, ref_date)
);
