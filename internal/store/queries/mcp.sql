-- name: CreateCandidate :one
INSERT INTO candidates (ticker, market, name, cnpj, cik, note, output, provenance, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetCandidate :one
SELECT * FROM candidates WHERE id = ?;

-- name: ListCandidates :many
SELECT * FROM candidates ORDER BY CASE status WHEN 'pending' THEN 0 ELSE 1 END, id DESC;

-- name: CountPendingCandidates :one
SELECT COUNT(*) FROM candidates WHERE status = 'pending';

-- name: DecideCandidate :one
UPDATE candidates SET status = ?, asset_id = ?, run_id = ?, decided_at = ? WHERE id = ? RETURNING *;

-- name: GetLookupCache :one
SELECT * FROM lookup_cache WHERE key = ?;

-- name: PutLookupCache :exec
INSERT INTO lookup_cache (key, payload, fetched_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET payload = excluded.payload, fetched_at = excluded.fetched_at;

-- name: CountLookupSymbolsOnDay :one
SELECT COUNT(*) FROM lookup_symbols WHERE day = ?;

-- name: HasLookupSymbol :one
SELECT COUNT(*) FROM lookup_symbols WHERE day = ? AND market = ? AND symbol = ?;

-- name: AddLookupSymbol :exec
INSERT OR IGNORE INTO lookup_symbols (day, market, symbol) VALUES (?, ?, ?);
