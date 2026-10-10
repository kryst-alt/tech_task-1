-- name: GetLeadByExternalIDForUpdate :one
SELECT * FROM leads WHERE external_id = $1 FOR UPDATE;

-- name: GetLeadByEmailForUpdate :one
SELECT * FROM leads WHERE email = $1 FOR UPDATE;

-- name: InsertLead :one
INSERT INTO leads (external_id, email, name, company, source)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateLeadFields :exec
UPDATE leads
SET email = $2, name = $3, company = $4, source = $5,
    version = version + 1, updated_at = now()
WHERE id = $1;

-- name: MarkLeadForSync :exec
INSERT INTO lead_sync (lead_id) VALUES ($1)
ON CONFLICT (lead_id) DO UPDATE SET attempts = 0, next_attempt_at = now(), last_error = NULL;
