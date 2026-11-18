-- name: CreateCashInstrument :one
INSERT INTO cash_instruments (kind, name, currency, pct_cdi) VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetCashInstrument :one
SELECT * FROM cash_instruments WHERE id = ?;

-- name: ListCashInstruments :many
SELECT * FROM cash_instruments ORDER BY id;

-- name: CreateCashMovement :one
INSERT INTO cash_movements (instrument_id, move_date, type, amount, quantity)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetCashMovement :one
SELECT * FROM cash_movements WHERE id = ?;

-- name: DeleteCashMovement :exec
DELETE FROM cash_movements WHERE id = ?;

-- name: ListCashMovements :many
SELECT * FROM cash_movements WHERE instrument_id = ? ORDER BY move_date, id;

-- name: UpsertCDIRate :exec
INSERT INTO cdi_rates (ref_date, rate_pct) VALUES (?, ?)
ON CONFLICT (ref_date) DO UPDATE SET rate_pct = excluded.rate_pct;

-- name: ListCDIRates :many
SELECT * FROM cdi_rates WHERE ref_date >= ? AND ref_date <= ? ORDER BY ref_date;

-- name: UpsertTesouroSelicPrice :exec
INSERT INTO tesouro_selic_prices (ref_date, price) VALUES (?, ?)
ON CONFLICT (ref_date) DO UPDATE SET price = excluded.price;

-- name: LatestTesouroSelicPrice :one
SELECT * FROM tesouro_selic_prices WHERE ref_date <= ? ORDER BY ref_date DESC LIMIT 1;

-- name: UpsertFXRate :exec
INSERT INTO fx_rates (pair, ref_date, rate) VALUES (?, ?, ?)
ON CONFLICT (pair, ref_date) DO UPDATE SET rate = excluded.rate;

-- name: LatestFXRate :one
SELECT * FROM fx_rates WHERE pair = ? AND ref_date <= ? ORDER BY ref_date DESC LIMIT 1;
