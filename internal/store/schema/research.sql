CREATE TABLE research_notes (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id               INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    body                   TEXT    NOT NULL,
    -- JSON array of strings (links or citations).
    sources                TEXT    NOT NULL DEFAULT '[]',
    author                 TEXT    NOT NULL DEFAULT 'user' CHECK (author IN ('user', 'ai_approved')),
    opportunity            INTEGER NOT NULL DEFAULT 0 CHECK (opportunity IN (0, 1)),
    -- Last day (inclusive) the opportunity flag counts; required when opportunity = 1.
    opportunity_expires_on TEXT,
    created_at             TEXT    NOT NULL,
    updated_at             TEXT    NOT NULL,
    CHECK (opportunity = 0 OR opportunity_expires_on IS NOT NULL)
);
CREATE INDEX research_notes_asset ON research_notes (asset_id, id);
