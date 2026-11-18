-- name: CreateAsset :one
INSERT INTO assets (ticker, name, market, currency, kind, sleeve, status, benchmark)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAsset :one
SELECT * FROM assets WHERE id = ?;

-- name: ListAssets :many
SELECT * FROM assets
WHERE (sqlc.narg('status') IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('sleeve') IS NULL OR sleeve = sqlc.narg('sleeve'))
  AND (sqlc.narg('market') IS NULL OR market = sqlc.narg('market'))
ORDER BY ticker, market;

-- name: UpdateAsset :one
UPDATE assets SET
    name = COALESCE(sqlc.narg('name'), name),
    sleeve = COALESCE(sqlc.narg('sleeve'), sleeve),
    benchmark = COALESCE(sqlc.narg('benchmark'), benchmark),
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdateAssetStatus :one
UPDATE assets SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?
RETURNING *;
