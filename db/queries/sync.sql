-- name: DueSyncs :many
SELECT l.* FROM lead_sync s JOIN leads l ON l.id = s.lead_id
WHERE l.version > s.synced_version AND s.next_attempt_at <= now()
ORDER BY s.next_attempt_at
LIMIT $1;

-- name: MarkSynced :exec
UPDATE lead_sync SET synced_version = greatest(synced_version, $2), attempts = 0, last_error = NULL WHERE lead_id = $1;

-- name: MarkSyncFailed :exec
UPDATE lead_sync
SET attempts = attempts + 1,
    last_error = @last_error::text,
    next_attempt_at = now() + least(interval '15 minutes', make_interval(secs => power(2, least(attempts + 1, 10))))
WHERE lead_id = @lead_id;

-- name: GetGoogleCredentials :one
SELECT * FROM google_credentials WHERE id = 1;

-- name: SaveGoogleCredentials :exec
-- A different account can't open the old sheet under drive.file, so it gets
-- a new sheet and every lead is queued to fill it.
WITH reset AS (
  UPDATE lead_sync SET synced_version = 0, attempts = 0, next_attempt_at = now(), last_error = NULL
  WHERE EXISTS (SELECT 1 FROM google_credentials WHERE id = 1 AND account_email <> @account_email)
)
INSERT INTO google_credentials (account_email, refresh_token_enc) VALUES (@account_email, @refresh_token_enc)
ON CONFLICT (id) DO UPDATE
SET refresh_token_enc = EXCLUDED.refresh_token_enc,
    spreadsheet_id = CASE WHEN google_credentials.account_email = EXCLUDED.account_email
                          THEN google_credentials.spreadsheet_id END,
    account_email = EXCLUDED.account_email,
    needs_reauth = false, updated_at = now();

-- name: SetSpreadsheetID :exec
UPDATE google_credentials SET spreadsheet_id = @spreadsheet_id::text WHERE id = 1;

-- name: MarkGoogleNeedsReauth :execrows
UPDATE google_credentials SET needs_reauth = true WHERE id = 1 AND NOT needs_reauth;
