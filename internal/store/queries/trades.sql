-- name: CreateTrade :one
INSERT INTO trades (asset_id, trade_date, side, quantity, price, fees, currency, fx_rate)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetTrade :one
SELECT * FROM trades WHERE id = ?;

-- name: ListTradesByAsset :many
SELECT * FROM trades WHERE asset_id = ? ORDER BY trade_date, id;

-- name: ListAllTrades :many
SELECT * FROM trades ORDER BY asset_id, trade_date, id;

-- name: DeleteTrade :exec
DELETE FROM trades WHERE id = ?;
