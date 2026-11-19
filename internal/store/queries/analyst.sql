-- name: CreateAnalystRun :one
INSERT INTO analyst_runs (asset_id, model, status, inputs, output, input_tokens, output_tokens, cost_usd, error, created_at, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAnalystRun :one
SELECT * FROM analyst_runs WHERE id = ?;

-- name: ListAnalystRunsByAsset :many
SELECT * FROM analyst_runs WHERE asset_id = ? ORDER BY id DESC;

-- name: UpdateAnalystRunStatus :one
UPDATE analyst_runs SET status = ?, approvals = ? WHERE id = ? RETURNING *;

-- name: ListAnalystCostsBetween :many
-- Only runs paid through the API count toward the monthly cap; MCP drafts cost 0.
SELECT cost_usd FROM analyst_runs WHERE created_at >= ? AND created_at < ? AND source = 'api';

-- name: CountMCPDraftRuns :one
SELECT COUNT(*) FROM analyst_runs WHERE source = 'mcp' AND status = 'draft';

-- name: ListMCPDraftRuns :many
SELECT * FROM analyst_runs WHERE source = 'mcp' AND status = 'draft' ORDER BY id DESC;
