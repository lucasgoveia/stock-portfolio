-- AI analyst runs (spec §4.2, §13). A run is a draft until the user approves
-- parts of it; nothing is applied automatically.
CREATE TABLE analyst_runs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id      INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    model         TEXT    NOT NULL,
    status        TEXT    NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'approved', 'discarded', 'failed')),
    -- JSON: what the model was given (thesis, documents used with title, url, date, ...).
    inputs        TEXT    NOT NULL DEFAULT '{}',
    -- JSON: the validated structured output; '{}' when the run failed.
    output        TEXT    NOT NULL DEFAULT '{}',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    -- Decimal as TEXT, USD, computed from the reported usage.
    cost_usd      TEXT    NOT NULL DEFAULT '0',
    -- JSON: what was approved so far (note ids, fair value ids).
    approvals     TEXT    NOT NULL DEFAULT '{}',
    error         TEXT    NOT NULL DEFAULT '',
    created_at    TEXT    NOT NULL
);
CREATE INDEX analyst_runs_asset ON analyst_runs (asset_id, id);
CREATE INDEX analyst_runs_created ON analyst_runs (created_at);
