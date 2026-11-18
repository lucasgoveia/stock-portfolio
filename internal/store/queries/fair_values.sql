-- name: CreateFairValue :one
INSERT INTO fair_values (asset_id, method, value_per_share, currency, as_of, inputs, sources, note, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetFairValue :one
SELECT * FROM fair_values WHERE id = ?;

-- name: ListFairValuesByAsset :many
SELECT * FROM fair_values WHERE asset_id = ? ORDER BY as_of DESC, id DESC;

-- name: LatestFairValue :one
SELECT * FROM fair_values WHERE asset_id = ? ORDER BY as_of DESC, id DESC LIMIT 1;

-- name: ListLatestFairValues :many
SELECT f.* FROM fair_values f
WHERE f.id = (
    SELECT g.id FROM fair_values g WHERE g.asset_id = f.asset_id ORDER BY g.as_of DESC, g.id DESC LIMIT 1
)
ORDER BY f.asset_id;

-- name: DeleteFairValue :execrows
DELETE FROM fair_values WHERE id = ?;
