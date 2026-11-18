-- name: CreateAnalystRun :one
INSERT INTO analyst_runs (asset_id, model, status, inputs, output, input_tokens, output_tokens, cost_usd, error, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAnalystRun :one
SELECT * FROM analyst_runs WHERE id = ?;

-- name: ListAnalystRunsByAsset :many
SELECT * FROM analyst_runs WHERE asset_id = ? ORDER BY id DESC;

-- name: UpdateAnalystRunStatus :one
UPDATE analyst_runs SET status = ?, approvals = ? WHERE id = ? RETURNING *;

-- name: ListAnalystCostsBetween :many
SELECT cost_usd FROM analyst_runs WHERE created_at >= ? AND created_at < ?;
