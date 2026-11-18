CREATE TABLE theses (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_id       INTEGER NOT NULL UNIQUE REFERENCES assets (id) ON DELETE CASCADE,
    body           TEXT    NOT NULL,
    status         TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'under_review', 'broken')),
    next_review_on TEXT,
    created_at     TEXT    NOT NULL,
    updated_at     TEXT    NOT NULL
);

CREATE TABLE thesis_break_conditions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    thesis_id  INTEGER NOT NULL REFERENCES theses (id) ON DELETE CASCADE,
    body       TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);
CREATE INDEX thesis_break_conditions_thesis ON thesis_break_conditions (thesis_id, id);

CREATE TABLE thesis_reviews (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    thesis_id      INTEGER NOT NULL REFERENCES theses (id) ON DELETE CASCADE,
    reviewed_on    TEXT    NOT NULL,
    note           TEXT    NOT NULL,
    status_after   TEXT    NOT NULL CHECK (status_after IN ('active', 'under_review', 'broken')),
    next_review_on TEXT,
    created_at     TEXT    NOT NULL
);
CREATE INDEX thesis_reviews_thesis ON thesis_reviews (thesis_id, reviewed_on, id);
