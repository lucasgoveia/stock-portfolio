-- name: CreateResearchNote :one
INSERT INTO research_notes (asset_id, body, sources, author, opportunity, opportunity_expires_on, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetResearchNote :one
SELECT * FROM research_notes WHERE id = ?;

-- name: ListResearchNotesByAsset :many
SELECT * FROM research_notes WHERE asset_id = ? ORDER BY created_at DESC, id DESC;

-- name: UpdateResearchNote :one
UPDATE research_notes
SET body = ?, sources = ?, opportunity = ?, opportunity_expires_on = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteResearchNote :execrows
DELETE FROM research_notes WHERE id = ?;

-- name: ListActiveOpportunities :many
SELECT * FROM research_notes
WHERE opportunity = 1 AND opportunity_expires_on >= sqlc.arg(asof)
  AND (sqlc.narg(asset_id) IS NULL OR asset_id = sqlc.narg(asset_id))
ORDER BY opportunity_expires_on, id;
