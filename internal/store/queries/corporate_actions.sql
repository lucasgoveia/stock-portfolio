-- name: CreateCorporateAction :one
INSERT INTO corporate_actions (asset_id, kind, ex_date, factor, cost_per_share, status, source, price_ratio, note)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertDetectedCorporateAction :execrows
INSERT INTO corporate_actions (asset_id, kind, ex_date, factor, cost_per_share, status, source, price_ratio, note)
VALUES (?, ?, ?, ?, ?, 'detected', ?, ?, ?)
ON CONFLICT (asset_id, kind, ex_date, source) WHERE source <> 'manual' DO NOTHING;

-- name: GetCorporateAction :one
SELECT * FROM corporate_actions WHERE id = ?;

-- name: ListCorporateActions :many
SELECT * FROM corporate_actions
WHERE (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(asset_id) IS NULL OR asset_id = sqlc.narg(asset_id))
ORDER BY ex_date DESC, id DESC;

-- name: ConfirmedCorporateActions :many
SELECT * FROM corporate_actions WHERE asset_id = ? AND status = 'confirmed' ORDER BY ex_date, id;

-- name: SetCorporateActionStatus :execrows
UPDATE corporate_actions SET status = ?, cost_per_share = ? WHERE id = ? AND status = 'detected';

-- name: DeleteCorporateAction :execrows
DELETE FROM corporate_actions WHERE id = ?;

-- name: ForceCorporateActionStatus :exec
UPDATE corporate_actions SET status = ? WHERE id = ?;
