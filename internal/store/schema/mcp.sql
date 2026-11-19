-- MCP server (spec §13.6): new-ticker proposals, the 24h lookup cache and the
-- daily count of distinct new symbols looked up.

-- A proposal to start tracking a ticker the app does not hold, submitted by the
-- user's Claude client. Nothing is created until the user accepts it.
CREATE TABLE candidates (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ticker      TEXT    NOT NULL,
    market      TEXT    NOT NULL CHECK (market IN ('B3', 'US')),
    name        TEXT    NOT NULL DEFAULT '',
    cnpj        TEXT    NOT NULL DEFAULT '',
    cik         TEXT    NOT NULL DEFAULT '',
    -- Research note (markdown) written by the model.
    note        TEXT    NOT NULL DEFAULT '',
    -- JSON: the proposed draft (scenarios, initiatives, fair value range,
    -- catalysts, sources) in the analyst output shape; '{}' when none.
    output      TEXT    NOT NULL DEFAULT '{}',
    -- JSON: client, model, documents read, external URLs.
    provenance  TEXT    NOT NULL DEFAULT '{}',
    status      TEXT    NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'accepted', 'rejected')),
    -- Set when accepted: the watchlist asset and the analyst run draft created.
    asset_id    INTEGER REFERENCES assets (id) ON DELETE SET NULL,
    run_id      INTEGER REFERENCES analyst_runs (id) ON DELETE SET NULL,
    created_at  TEXT    NOT NULL,
    decided_at  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX candidates_status ON candidates (status, id);

-- Cached external lookups made by MCP tools (24h): the CVM register, ticker to
-- CIK resolutions, unknown-ticker filing lists and statements.
CREATE TABLE lookup_cache (
    key        TEXT PRIMARY KEY,
    payload    TEXT NOT NULL,
    fetched_at TEXT NOT NULL
);

-- Distinct symbols not held by the app that were looked up on a day
-- (America/Sao_Paulo); capped by the settings parameter.
CREATE TABLE lookup_symbols (
    day    TEXT NOT NULL,
    market TEXT NOT NULL,
    symbol TEXT NOT NULL,
    PRIMARY KEY (day, market, symbol)
);
