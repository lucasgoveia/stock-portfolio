-- A stored contribution plan (spec §8.4). plan holds the planner output as
-- JSON; execution is tracked per order in contribution_orders. Status is
-- derived from the orders unless the plan was discarded.
CREATE TABLE contributions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    amount       TEXT    NOT NULL,
    plan         TEXT    NOT NULL,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    discarded_at TEXT
);

CREATE TABLE contribution_orders (
    contribution_id INTEGER NOT NULL REFERENCES contributions (id) ON DELETE CASCADE,
    seq             INTEGER NOT NULL,
    status          TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'skipped')),
    trade_id        INTEGER REFERENCES trades (id) ON DELETE SET NULL,
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (contribution_id, seq)
);
