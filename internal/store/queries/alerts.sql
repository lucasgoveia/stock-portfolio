-- name: UpsertAlert :one
INSERT INTO alerts (type, asset_id, dedupe_key, payload, state, created_at, updated_at)
VALUES (?, ?, ?, ?, 'open', ?, ?)
ON CONFLICT (dedupe_key) WHERE state <> 'resolved'
DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at
RETURNING *;

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = ?;

-- name: ListActiveAlertsByType :many
SELECT * FROM alerts WHERE type = ? AND state <> 'resolved';

-- name: SetAlertState :one
UPDATE alerts
SET state = ?, snoozed_until = ?, updated_at = ?, resolved_at = ?
WHERE id = ? AND state <> 'resolved'
RETURNING *;

-- name: ListAlerts :many
-- A snoozed alert whose snoozed_until has passed counts as open (computed at read time).
SELECT * FROM alerts
WHERE (CASE WHEN state = 'snoozed' AND snoozed_until <= sqlc.arg(now) THEN 'open' ELSE state END)
        = sqlc.arg(state)
  AND (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(asset_id) IS NULL OR asset_id = sqlc.narg(asset_id))
ORDER BY created_at DESC, id DESC;

-- name: ListAllAlerts :many
SELECT * FROM alerts
WHERE (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(asset_id) IS NULL OR asset_id = sqlc.narg(asset_id))
ORDER BY created_at DESC, id DESC;
