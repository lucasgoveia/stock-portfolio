-- name: UpsertAssetPrice :exec
INSERT INTO asset_prices (asset_id, ref_date, source, close, adj_close, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (asset_id, ref_date, source) DO UPDATE
SET close = excluded.close, adj_close = excluded.adj_close, fetched_at = excluded.fetched_at;

-- name: LatestAssetPrices :many
-- Newest close per asset; on the same date raw sources win over brapi.
SELECT p.* FROM asset_prices p
WHERE p.rowid = (
    SELECT q.rowid FROM asset_prices q
    WHERE q.asset_id = p.asset_id
    ORDER BY q.ref_date DESC,
             CASE q.source WHEN 'manual' THEN 0 WHEN 'cotahist' THEN 1 WHEN 'tiingo' THEN 2 ELSE 3 END
    LIMIT 1
)
ORDER BY p.asset_id;

-- name: AssetPriceHistory :many
SELECT * FROM asset_prices
WHERE asset_id = ? AND source = ? AND ref_date >= sqlc.arg(from_date) AND ref_date <= sqlc.arg(to_date)
ORDER BY ref_date;

-- name: CountAssetPrices :one
SELECT COUNT(*) FROM asset_prices;

-- name: UpsertIndexPoint :exec
INSERT INTO index_points (index_code, ref_date, close) VALUES (?, ?, ?)
ON CONFLICT (index_code, ref_date) DO UPDATE SET close = excluded.close;

-- name: IndexHistory :many
SELECT * FROM index_points
WHERE index_code = ? AND ref_date >= sqlc.arg(from_date) AND ref_date <= sqlc.arg(to_date)
ORDER BY ref_date;

-- name: LatestIndexDate :one
SELECT CAST(COALESCE(MAX(ref_date), '') AS TEXT) FROM index_points WHERE index_code = ?;

-- name: CreateJobRun :one
INSERT INTO job_runs (job, run_date, started_at, status) VALUES (?, ?, ?, 'running')
RETURNING *;

-- name: FinishJobRun :exec
UPDATE job_runs SET finished_at = ?, status = ?, steps = ? WHERE id = ?;

-- name: LastJobRun :one
SELECT * FROM job_runs WHERE job = ? ORDER BY id DESC LIMIT 1;

-- name: EarliestIndexDate :one
SELECT CAST(COALESCE(MIN(ref_date), '') AS TEXT) FROM index_points WHERE index_code = ?;

-- name: ListHistoryBackfills :many
SELECT subject FROM history_backfills WHERE source = ?;

-- name: UpsertHistoryBackfill :exec
INSERT INTO history_backfills (subject, source, from_date, done_at) VALUES (?, ?, ?, ?)
ON CONFLICT (subject, source) DO UPDATE SET from_date = excluded.from_date, done_at = excluded.done_at;

-- name: UpsertRiskFreeRate :exec
INSERT INTO risk_free_rates (code, ref_date, rate) VALUES (?, ?, ?)
ON CONFLICT (code, ref_date) DO UPDATE SET rate = excluded.rate;

-- name: LatestRiskFreeRate :one
SELECT * FROM risk_free_rates WHERE code = ? ORDER BY ref_date DESC LIMIT 1;
