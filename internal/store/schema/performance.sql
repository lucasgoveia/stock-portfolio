-- Daily values for time-weighted return (spec §15). segment is 'total',
-- 'core', 'tactical', 'br', 'us' or 'asset:<id>'. flow is the external
-- money that entered (positive) or left (negative) the segment that day,
-- in BRL; dividends are internal and never a flow.
CREATE TABLE performance_snapshots (
    ref_date TEXT NOT NULL,
    segment  TEXT NOT NULL,
    value    TEXT NOT NULL,
    flow     TEXT NOT NULL DEFAULT '0',
    PRIMARY KEY (ref_date, segment)
);
