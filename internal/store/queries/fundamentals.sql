-- name: UpsertFundamentalFact :exec
INSERT INTO fundamental_facts (asset_id, period_end, period_type, concept, value, source, filed)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (asset_id, period_end, period_type, concept)
DO UPDATE SET value = excluded.value, source = excluded.source, filed = excluded.filed;

-- name: ListFundamentalFacts :many
SELECT * FROM fundamental_facts WHERE asset_id = ? ORDER BY period_end, period_type, concept;

-- name: DeleteFundamentalTTM :exec
DELETE FROM fundamental_facts WHERE asset_id = ? AND period_type = 'ttm';

-- name: CountAnnualFundamentalPeriods :one
SELECT COUNT(DISTINCT period_end) FROM fundamental_facts
WHERE asset_id = ? AND period_type = 'annual' AND concept = 'revenue';

-- name: UpsertFundamentalSync :exec
INSERT INTO fundamental_syncs (asset_id, source, fetched_at, as_of, status, message, notes)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (asset_id) DO UPDATE
SET source = excluded.source, fetched_at = excluded.fetched_at, as_of = excluded.as_of,
    status = excluded.status, message = excluded.message, notes = excluded.notes;

-- name: GetFundamentalSync :one
SELECT * FROM fundamental_syncs WHERE asset_id = ?;

-- name: ListFundamentalSyncs :many
SELECT * FROM fundamental_syncs;

-- name: LatestAssetPrice :one
-- Newest close of one asset; on the same date raw sources win over brapi.
SELECT * FROM asset_prices WHERE asset_id = ?
ORDER BY ref_date DESC,
         CASE source WHEN 'manual' THEN 0 WHEN 'cotahist' THEN 1 WHEN 'tiingo' THEN 2 ELSE 3 END
LIMIT 1;

-- name: AssetPriceOnOrBefore :one
-- Last close on or before a date, not older than from_date.
SELECT * FROM asset_prices
WHERE asset_id = ? AND ref_date <= sqlc.arg(on_date) AND ref_date >= sqlc.arg(from_date)
ORDER BY ref_date DESC,
         CASE source WHEN 'manual' THEN 0 WHEN 'cotahist' THEN 1 WHEN 'tiingo' THEN 2 ELSE 3 END
LIMIT 1;

-- name: TrailingDividendsPerShare :many
SELECT per_share FROM dividends
WHERE asset_id = ? AND status <> 'rejected' AND record_date > sqlc.arg(from_date) AND record_date <= sqlc.arg(to_date);
