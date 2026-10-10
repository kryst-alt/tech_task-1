# Stack and approach

Status: proposed on 2026-10-10. Language, framework and database were the user's choices. Everything marked **default** is a decision Claude made that the user can override.

## Architecture

One Go service in one repository, built into one binary. The binary runs the HTTP API and the Google Sheets sync worker as two goroutines that share a Postgres pool.

```
partner ──signed POST──▶ API (Fiber) ──tx──▶ Postgres: leads, lead_sync
                                                   │
                         sync worker ◀──polls──────┘──▶ Google Sheets API
```

### Why not microservices

| | Single service + worker | Microservices (API, sync, auth) |
|---|---|---|
| Moving parts | 1 binary, 1 database | 3 deployables, a broker or service-to-service HTTP, shared auth state |
| Google outage while a lead arrives | Lead is saved and the API returns 2xx. The worker retries later. | Same outcome, with more parts in between |
| Testing R4 | `go test` against one real Postgres | Several services to start for each test |
| Scaling needs here | One webhook endpoint at partner traffic | Not needed |

The only isolation this system needs is between "accept the lead" and "talk to Google". The worker gives that. If the worker ever needs its own process, add `cmd/worker` that calls the same package. Nothing in the data model changes.

## Libraries

| Concern | Choice |
|---|---|
| Language | Go (current stable) |
| HTTP | Fiber v3. Confirm the latest stable tag when scaffolding. |
| Postgres driver | `jackc/pgx/v5` with `pgxpool` |
| Queries | `sqlc` generates typed Go from plain SQL |
| Migrations | `pressly/goose`, SQL files under `db/migrations` |
| Google | `golang.org/x/oauth2`, `google.golang.org/api/sheets/v4` |
| Logging | `log/slog` |
| Config | Environment variables, parsed and validated once at startup |
| Tests | `testing`, `testcontainers-go` (Postgres module), Fiber `app.Test` |
| Local run | Docker Compose: `postgres:17` plus the app |

## Data model

```sql
CREATE TYPE lead_stage AS ENUM ('new', 'qualified', 'proposal', 'won', 'lost');

CREATE TABLE leads (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  external_id  text NOT NULL UNIQUE,          -- the partner's lead ID
  email        citext NOT NULL UNIQUE,         -- case-insensitive match
  name         text NOT NULL,
  company      text NOT NULL,
  source       text NOT NULL,
  stage        lead_stage NOT NULL DEFAULT 'new',
  version      bigint NOT NULL DEFAULT 1,      -- bumped on every change
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lead_sync (                       -- one row per lead, not per event
  lead_id          uuid PRIMARY KEY REFERENCES leads(id),
  synced_version   bigint NOT NULL DEFAULT 0,
  attempts         int NOT NULL DEFAULT 0,
  next_attempt_at  timestamptz NOT NULL DEFAULT now(),
  last_error       text
);

CREATE TABLE google_credentials (              -- single row
  id                 smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  account_email      text NOT NULL,
  refresh_token_enc  bytea NOT NULL,           -- AES-256-GCM, key from env
  spreadsheet_id     text,
  updated_at         timestamptz NOT NULL DEFAULT now()
);
```

The worker syncs a lead whenever `leads.version > lead_sync.synced_version`. It writes the lead's current row rather than replaying events. Three updates in a row therefore produce one write, and a crash mid-sync is fixed by the next pass.

## Receiving a lead

`POST /v1/leads`

1. **Verify the signature (default scheme).** Headers `X-Timestamp` (Unix seconds) and `X-Signature: sha256=<hex>`, where the hex is HMAC-SHA256 with the shared secret over `timestamp + "." + raw body`. Compare with `hmac.Equal`. Return 401 when a header is missing, the signature doesn't match, or the timestamp is more than 5 minutes off. This runs before the JSON is parsed.
2. **Parse and validate.** All five fields are required. The email must parse with `net/mail`. Return 422 on bad input.
3. **Upsert in one transaction (default matching rule).**
   - Look up rows by `external_id` and by `email`.
   - No match creates the lead with stage `new`. Return 201.
   - One lead matches, by either key. Update name, company, source and email when they differ. When nothing changed, write nothing and return 200. Otherwise bump `version` and return 200.
   - The `external_id` matches one lead and the `email` matches a different one. Return 409 and change nothing.
   - Incoming leads never change `stage`.
   - The unique indexes catch two concurrent inserts. The loser of the race retries once and takes the update path.
4. **Mark it for sync** in the same transaction by upserting `lead_sync` with `next_attempt_at = now()`.

## Deal stage

The allowed moves live in one transition table in the `lead` package. Handlers never check stages with `if` chains.

| From | Allowed to |
|---|---|
| new | qualified, lost |
| qualified | proposal, lost |
| proposal | won, lost |
| won | nothing (final) |
| lost | nothing (final) |

`PATCH /v1/leads/{id}/stage` with `{"stage": "qualified"}` returns 200, or 409 for a move the table doesn't allow. `{id}` is the partner's lead ID, the one shown in the sheet. The change bumps `version`, so the sheet picks it up. **Default.** The brief doesn't say how stages change. This endpoint is the smallest way to make the stage field useful.

The endpoint requires `Authorization: Bearer <ADMIN_API_KEY>`, compared in constant time. **Default.** The partner signature only covers lead intake, and an unauthenticated write endpoint would let anyone move deals.

## Google Sheets sync

**Sign-in.** `GET /auth/google` redirects to Google with `access_type=offline` and `prompt=consent`, so Google returns a refresh token. `GET /auth/google/callback` encrypts the refresh token and stores it in `google_credentials`. The user signs in once.

**Staying signed in (R3.3).** At each sync the worker builds an `oauth2.TokenSource` from the stored refresh token. The library refreshes the access token, which lasts one hour, whenever it expires. If Google returns `invalid_grant`, the token was revoked. The worker then stops syncing, logs an error, and `GET /healthz` reports `google: reauth_required` until someone signs in again.

**Scope (default).** `drive.file`, with the app creating the spreadsheet itself on first sign-in. This scope only reaches files the app created. Claude's understanding is that Google treats it as non-sensitive, while the full `spreadsheets` scope needs app verification. Confirm this in the Google Cloud console during setup.

**Token expiry trap.** While the OAuth consent screen is in "Testing" status, Google expires refresh tokens after 7 days. Set it to "In production" for anything longer than a demo.

**Sheet layout (default).** One tab named `Leads` with columns `Lead ID, Name, Email, Company, Source, Stage, Created, Updated`. The worker reads column A to find the lead's row. It updates that row when it exists and appends one when it doesn't. Running the same sync twice leaves the same sheet.

**Worker loop.** Every 2 seconds, claim up to 50 due rows with `FOR UPDATE SKIP LOCKED`, sync each, and set `synced_version`. On failure the worker increments `attempts`, records `last_error`, and pushes `next_attempt_at` back exponentially, capped at 15 minutes.

## Code layout

```
cmd/server/main.go        loads config, opens the pool, starts the API and the worker
internal/config/          environment parsing and validation
internal/lead/            Lead type, Stage transition table, upsert decision (pure Go, no I/O)
internal/store/           pool and migrations, sqlc queries, the upsert transaction
internal/api/             Fiber app and every HTTP handler, including the signature check
internal/sheets/          OAuth token storage and the sync worker
internal/testdb/          one Postgres container per test binary
db/migrations/            goose SQL, embedded into the binary and applied at startup
db/queries/               sqlc SQL
```

The `lead` package holds the decisions (create, update, no-op, conflict, allowed stage move) as pure functions. They can be unit tested without a database. Fiber and pgx stay at the edges.

## Tests

| Requirement | Test |
|---|---|
| R4.1 | POST with a bad signature, a missing header and a stale timestamp. Each returns 401 and the `leads` count stays 0. |
| R4.2 | POST the same signed lead twice, then twice more in parallel. One row exists and the responses are 201, then 200. |
| R4.3 | POST lead A, then a lead with a new `external_id` and A's email in different case. One row exists with the new name and `version = 2`. |
| Extra | ID/email conflict returns 409. Invalid stage moves return 409. The sync worker, run against a fake Sheets client, converges after an injected failure. |

The endpoint tests drive the real Fiber app with `app.Test` against Postgres in testcontainers. `go test ./...` runs everything. It needs Docker running.

## Configuration

`DATABASE_URL`, `LEAD_SIGNING_SECRET`, `ADMIN_API_KEY`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL`, `TOKEN_ENCRYPTION_KEY` (32 bytes, base64), `PORT`. The app fails at startup if any is missing or malformed.
