-- name: CreateDividend :one
INSERT INTO dividends (asset_id, kind, record_date, pay_date, quantity, per_share, gross, withholding, net,
                       currency, fx_rate, status, source, movement_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertDetectedDividend :execrows
INSERT INTO dividends (asset_id, kind, record_date, pay_date, quantity, per_share, gross, withholding, net,
                       currency, fx_rate, status, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'detected', ?)
ON CONFLICT (asset_id, kind, record_date, source) WHERE source <> 'manual' DO UPDATE SET
  pay_date = excluded.pay_date, quantity = excluded.quantity, per_share = excluded.per_share,
  gross = excluded.gross, withholding = excluded.withholding, net = excluded.net,
  currency = excluded.currency, fx_rate = excluded.fx_rate
WHERE dividends.status = 'detected';

-- name: GetDividend :one
SELECT * FROM dividends WHERE id = ?;

-- name: ListDividends :many
SELECT * FROM dividends
WHERE (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(asset_id) IS NULL OR asset_id = sqlc.narg(asset_id))
ORDER BY record_date DESC, id DESC;

-- name: ConfirmDividend :exec
UPDATE dividends
SET status = 'confirmed', pay_date = ?, withholding = ?, net = ?, fx_rate = ?, movement_id = ?
WHERE id = ?;

-- name: RejectDividend :execrows
UPDATE dividends SET status = 'rejected' WHERE id = ? AND status = 'detected';

-- name: DeleteDividend :execrows
DELETE FROM dividends WHERE id = ?;

-- name: UndeployedDividends :many
SELECT * FROM dividends WHERE status = 'confirmed' AND contribution_id IS NULL ORDER BY id;

-- name: MarkDividendsDeployed :exec
UPDATE dividends SET contribution_id = ? WHERE status = 'confirmed' AND contribution_id IS NULL;
