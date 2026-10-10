-- name: ClaimDueSyncs :many
SELECT l.* FROM lead_sync s JOIN leads l ON l.id = s.lead_id
WHERE l.version > s.synced_version AND s.next_attempt_at <= now()
ORDER BY s.next_attempt_at
LIMIT $1
FOR UPDATE OF s SKIP LOCKED;

-- name: MarkSynced :exec
UPDATE lead_sync SET synced_version = $2, attempts = 0, last_error = NULL WHERE lead_id = $1;

-- name: MarkSyncFailed :exec
UPDATE lead_sync
SET attempts = attempts + 1,
    last_error = @last_error::text,
    next_attempt_at = now() + least(interval '15 minutes', make_interval(secs => power(2, attempts + 1)))
WHERE lead_id = @lead_id;

-- name: GetGoogleCredentials :one
SELECT * FROM google_credentials WHERE id = 1;

-- name: SaveGoogleCredentials :exec
INSERT INTO google_credentials (account_email, refresh_token_enc) VALUES ($1, $2)
ON CONFLICT (id) DO UPDATE
SET account_email = EXCLUDED.account_email, refresh_token_enc = EXCLUDED.refresh_token_enc,
    needs_reauth = false, updated_at = now();

-- name: SetSpreadsheetID :exec
UPDATE google_credentials SET spreadsheet_id = @spreadsheet_id::text WHERE id = 1;

-- name: MarkGoogleNeedsReauth :exec
UPDATE google_credentials SET needs_reauth = true WHERE id = 1;
