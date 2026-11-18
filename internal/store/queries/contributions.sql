-- name: CreateContribution :one
INSERT INTO contributions (amount, plan) VALUES (?, ?)
RETURNING *;

-- name: GetContribution :one
SELECT * FROM contributions WHERE id = ?;

-- name: ListContributions :many
SELECT * FROM contributions ORDER BY id DESC LIMIT ?;

-- name: DiscardContribution :execrows
UPDATE contributions SET discarded_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ? AND discarded_at IS NULL;

-- name: CreateContributionOrder :exec
INSERT INTO contribution_orders (contribution_id, seq) VALUES (?, ?);

-- name: ListContributionOrders :many
SELECT * FROM contribution_orders WHERE contribution_id = ? ORDER BY seq;

-- name: SetContributionOrder :execrows
UPDATE contribution_orders
SET status = ?, trade_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE contribution_id = ? AND seq = ?;
