CREATE TABLE alerts (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    type          TEXT    NOT NULL,
    asset_id      INTEGER REFERENCES assets (id) ON DELETE CASCADE,
    dedupe_key    TEXT    NOT NULL,
    payload       TEXT    NOT NULL DEFAULT '{}',
    state         TEXT    NOT NULL DEFAULT 'open'
                  CHECK (state IN ('open', 'snoozed', 'acknowledged', 'resolved')),
    snoozed_until TEXT,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL,
    resolved_at   TEXT
);

-- At most one non-resolved alert per dedupe key; resolved ones are history.
CREATE UNIQUE INDEX alerts_dedupe_active ON alerts (dedupe_key) WHERE state <> 'resolved';
CREATE INDEX alerts_state ON alerts (state);
