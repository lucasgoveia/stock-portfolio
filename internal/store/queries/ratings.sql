-- name: CreateRating :one
INSERT INTO ratings (asset_id, rated_on, volatility, understanding, moat, predictability, horizon, reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListRatingsByAsset :many
SELECT * FROM ratings WHERE asset_id = ? ORDER BY rated_on DESC, id DESC;

-- name: LatestRatingByAsset :one
SELECT * FROM ratings WHERE asset_id = ? ORDER BY rated_on DESC, id DESC LIMIT 1;
