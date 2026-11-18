-- name: CreateRating :one
INSERT INTO ratings (asset_id, rated_on, volatility, understanding, moat, predictability, horizon, reason, volatility_auto)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRatingsByAsset :many
SELECT * FROM ratings WHERE asset_id = ? ORDER BY rated_on DESC, id DESC;

-- name: LatestRatingByAsset :one
SELECT * FROM ratings WHERE asset_id = ? ORDER BY rated_on DESC, id DESC LIMIT 1;

-- name: UpsertAssetBeta :exec
INSERT INTO asset_betas (asset_id, index_code, beta, weeks, computed_on) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (asset_id) DO UPDATE
SET index_code = excluded.index_code, beta = excluded.beta, weeks = excluded.weeks, computed_on = excluded.computed_on;

-- name: GetAssetBeta :one
SELECT * FROM asset_betas WHERE asset_id = ?;
