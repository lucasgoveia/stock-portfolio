-- name: UpsertPerformanceSnapshot :exec
INSERT INTO performance_snapshots (ref_date, segment, value, flow) VALUES (?, ?, ?, ?)
ON CONFLICT (ref_date, segment) DO UPDATE SET value = excluded.value, flow = excluded.flow;

-- name: DeletePerformanceDay :exec
DELETE FROM performance_snapshots WHERE ref_date = ?;

-- name: PerformanceSeries :many
SELECT * FROM performance_snapshots
WHERE segment = ? AND ref_date >= sqlc.arg(from_date) AND ref_date <= sqlc.arg(to_date)
ORDER BY ref_date;

-- name: PerformanceSegments :many
SELECT DISTINCT segment FROM performance_snapshots ORDER BY segment;

-- name: AssetPriceAt :one
-- Latest close on or before a date; raw sources win on the same date.
SELECT * FROM asset_prices
WHERE asset_id = ? AND ref_date <= ?
ORDER BY ref_date DESC,
         CASE source WHEN 'manual' THEN 0 WHEN 'cotahist' THEN 1 WHEN 'tiingo' THEN 2 ELSE 3 END
LIMIT 1;

-- name: CashFlowsOn :many
-- Deposits and withdrawals of a day that are not dividend credits.
SELECT m.type, m.amount, i.currency FROM cash_movements m
JOIN cash_instruments i ON i.id = m.instrument_id
WHERE m.move_date = ? AND m.type IN ('deposit', 'withdraw')
  AND NOT EXISTS (SELECT 1 FROM dividends d WHERE d.movement_id = m.id);

-- name: FirstActivityDate :one
SELECT CAST(COALESCE(MIN(d), '') AS TEXT) FROM (
    SELECT MIN(trade_date) AS d FROM trades
    UNION ALL SELECT MIN(move_date) FROM cash_movements
);
