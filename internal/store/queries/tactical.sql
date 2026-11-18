-- name: UpsertTacticalSetup :one
INSERT INTO tactical_setups (asset_id, note_id, entry_price, target_price, stop_price, expires_on)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (asset_id) DO UPDATE SET
    note_id = excluded.note_id, entry_price = excluded.entry_price, target_price = excluded.target_price,
    stop_price = excluded.stop_price, expires_on = excluded.expires_on,
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
RETURNING *;

-- name: GetTacticalSetup :one
SELECT * FROM tactical_setups WHERE asset_id = ?;

-- name: ListTacticalSetups :many
SELECT * FROM tactical_setups ORDER BY asset_id;

-- name: DeleteTacticalSetup :execrows
DELETE FROM tactical_setups WHERE asset_id = ?;
