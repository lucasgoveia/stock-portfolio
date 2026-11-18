-- name: CreateThesis :one
INSERT INTO theses (asset_id, body, status, next_review_on, created_at, updated_at)
VALUES (?, ?, 'active', ?, ?, ?)
RETURNING *;

-- name: GetThesisByAsset :one
SELECT * FROM theses WHERE asset_id = ?;

-- name: ListTheses :many
SELECT * FROM theses ORDER BY id;

-- name: UpdateThesis :one
UPDATE theses SET body = ?, next_review_on = ?, updated_at = ? WHERE id = ?
RETURNING *;

-- name: SetThesisStatus :one
UPDATE theses SET status = ?, updated_at = ? WHERE id = ?
RETURNING *;

-- name: SetThesisReview :one
UPDATE theses SET status = ?, next_review_on = ?, updated_at = ? WHERE id = ?
RETURNING *;

-- name: DeleteThesis :exec
DELETE FROM theses WHERE id = ?;

-- name: CreateBreakCondition :one
INSERT INTO thesis_break_conditions (thesis_id, body, created_at, updated_at) VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetBreakCondition :one
SELECT * FROM thesis_break_conditions WHERE id = ?;

-- name: ListBreakConditions :many
SELECT * FROM thesis_break_conditions WHERE thesis_id = ? ORDER BY id;

-- name: UpdateBreakCondition :one
UPDATE thesis_break_conditions SET body = ?, updated_at = ? WHERE id = ?
RETURNING *;

-- name: DeleteBreakCondition :execrows
DELETE FROM thesis_break_conditions WHERE id = ?;

-- name: CreateThesisReview :one
INSERT INTO thesis_reviews (thesis_id, reviewed_on, note, status_after, next_review_on, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListThesisReviews :many
SELECT * FROM thesis_reviews WHERE thesis_id = ? ORDER BY reviewed_on DESC, id DESC;
