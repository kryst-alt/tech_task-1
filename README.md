# Lead Management Service

Receives leads from a partner website, checks they are authentic, stores each lead once in Postgres, and keeps a Google Sheet in sync.

Two Go services share one Postgres database:

```
partner ──signed POST──▶ lead-api ──▶ Postgres ◀── sheets-sync ──▶ Google Sheets
                            ▲
admin ── stage changes, Google sign-in
```

- **lead-api** verifies and stores leads, changes deal stages, and runs the Google sign-in.
- **sheets-sync** writes new and changed leads to the sheet, retrying with backoff when Google fails.

Design notes are in [docs/STACK.md](docs/STACK.md), and the requirements are in [docs/PROJECT.md](docs/PROJECT.md).

## Status

| Feature | State |
|---|---|
| Signed lead endpoint with duplicate and email handling | Done |
| Tests for invalid signature, duplicates and email update | Done |
| Deal stages in the database | Done |
| Endpoint to change a lead's stage | Done |
| Google sign-in and Sheets sync | Done |

## Requirements

- Podman with `podman-compose`, or Docker with Compose
- `openssl`, `curl`, and `python3` (setup reads the Google JSON with it)
- Go 1.26 or newer, only for running tests or the services outside containers

On Fedora:

```sh
sudo dnf install golang podman-compose
systemctl --user enable --now podman.socket   # make test reaches Podman through this socket
```

## Quick start

1. Put the Google OAuth client JSON in the project folder. See [Google Sheets setup](#google-sheets-setup) for how to get it.
2. Run setup. It creates `.env`, generates the secrets, and imports the Google client. Running it again never overwrites a value.
   ```sh
   make setup
   ```
3. Start everything:
   ```sh
   make up       # db, lead-api and sheets-sync; make ps shows their status
   ```
4. Link Google once with the URL that setup printed: `http://localhost:8080/auth/google?key=<ADMIN_API_KEY>`.
5. Send a lead:
   ```sh
   scripts/send-lead.sh '{"id":"p-1","name":"Ada","email":"ada@example.com","company":"Acme","source":"partner"}'
   ```
   It returns `201 {"result":"create"}` and appears in the sheet within a few seconds.

`make logs` follows both services. `make down` stops them and keeps the data. `podman compose down -v` deletes the data too.

Run `make help` for every target.

### Running the services without containers

```sh
make db-up      # Postgres only
make run-api    # terminal 1
make run-sync   # terminal 2
```

## Google Sheets setup

Done once per deployment.

1. In the [Google Cloud console](https://console.cloud.google.com/), create a project and enable the **Google Sheets API** and **Google Drive API**.
2. Under **OAuth consent screen**, choose External and add the Google account that should own the sheet under **Test users**.
3. Create **Credentials > OAuth client ID > Web application** with the redirect URI `http://localhost:8080/auth/google/callback`.
4. Download the client JSON into the project folder and run `make setup`, which copies the ID and secret into `.env`. The `client_secret_*.json` file is git-ignored. Don't commit it.
5. Start the services and open `http://localhost:8080/auth/google?key=<ADMIN_API_KEY>` in a browser on the same machine. Sign in with the test user account.

The app creates a spreadsheet called **Leads** in that account's Drive and shows its link. Leads already in the database sync within a few seconds. `GET /healthz` reports `"google":"ok"`.

The server stores the refresh token encrypted and renews the hourly access token by itself, so nobody signs in again after a restart or the next day. Two cases do need a new sign-in:

- Google revokes access, for example after a password change. Sync pauses, and `/healthz` reports `"google":"reauth_required"`. Open the sign-in link again.
- The consent screen is still in **Testing** status. Google expires refresh tokens after 7 days in that state. Set it to **In production** for anything longer than a demo.

## Tests

```sh
make test
```

The tests start their own Postgres container, so `make db-up` is not needed. The Makefile points testcontainers at the Podman socket when it exists, and at Docker otherwise.

| Test | Proves |
|---|---|
| `TestLeadWithInvalidSignatureIsRejected` | A missing, wrong or stale signature returns 401 and saves nothing |
| `TestDuplicateSubmissionCreatesOneRecord` | Sending the same lead twice stores one row |
| `TestLeadWithExistingEmailUpdatesRecord` | A new ID with a known email updates that lead |
| `TestUpsertLosingInsertRaceBecomesUpdate` | Two simultaneous first submissions still store one row |
| `internal/sheets` tests | The worker writes each change once, retries with backoff, waits while unlinked, and pauses on a revoked token |

## API

### `POST /v1/leads`

```json
{"id": "p-1", "name": "Ada", "email": "ada@example.com", "company": "Acme", "source": "partner"}
```

Every request needs two headers:

| Header | Value |
|---|---|
| `X-Timestamp` | Current Unix time in seconds. Requests more than 5 minutes off are rejected. |
| `X-Signature` | `sha256=` followed by the hex HMAC-SHA256 of `<timestamp>.<raw body>`, keyed with `LEAD_SIGNING_SECRET` |

Signing in shell:

```sh
ts=$(date +%s)
sig=$(printf '%s' "$ts.$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $NF}')
```

| Status | When |
|---|---|
| 201 `create` | New lead |
| 200 `update` | The ID or email matches a stored lead and some field changed |
| 200 `unchanged` | The lead matches a stored lead exactly |
| 401 | Signature missing, invalid or stale |
| 409 | The ID belongs to one stored lead and the email to another |
| 422 | Body is not JSON, a field is missing, or the email is invalid |

Email matching ignores case. An incoming lead never changes the deal stage.

### `PATCH /v1/leads/{id}/stage`

Moves a lead to another deal stage. `{id}` is the partner's lead ID. Needs `Authorization: Bearer <ADMIN_API_KEY>`.

```sh
curl -X PATCH localhost:8080/v1/leads/p-1/stage \
  -H "Authorization: Bearer $(grep ^ADMIN_API_KEY= .env | cut -d= -f2-)" \
  -d '{"stage":"qualified"}'
```

Allowed moves: `new` to `qualified`, `qualified` to `proposal`, `proposal` to `won`, and any open stage to `lost`. `won` and `lost` are final.

| Status | When |
|---|---|
| 200 `{"stage":"qualified"}` | Moved |
| 401 | Token missing or wrong |
| 404 | No lead with that ID |
| 409 | The move is not allowed from the current stage |
| 422 | Unknown stage or body is not JSON |

### `GET /auth/google?key=<ADMIN_API_KEY>`

Starts Google sign-in. See [Google Sheets setup](#google-sheets-setup). The key goes in the URL because a browser link can't send an Authorization header.

### `GET /healthz`

Returns `200 {"database":"ok","google":"ok"}`. `google` is `not_linked` before sign-in and `reauth_required` after Google revokes access. Returns 503 when Postgres is unreachable.

## Configuration

All settings live in `.env`. `make setup` fills in the required ones.

| Variable | Used by | Default | Purpose |
|---|---|---|---|
| `DATABASE_URL` | both | required | Postgres connection string. Compose sets it for the containers. |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | both | required | Google OAuth client |
| `TOKEN_ENCRYPTION_KEY` | both | required | 32 bytes, base64. Encrypts the stored Google refresh token. |
| `LEAD_SIGNING_SECRET` | lead-api | required | Shared HMAC secret, at least 32 characters |
| `ADMIN_API_KEY` | lead-api | required | Bearer token for admin endpoints and the sign-in link, at least 32 characters |
| `PORT` | lead-api | `8080` | HTTP port on the host |
| `DB_PORT` | compose | `5432` | Postgres port on the host |
| `GOOGLE_REDIRECT_URL` | lead-api | `http://localhost:$PORT/auth/google/callback` | Must match the redirect URI on the OAuth client |
| `SIGNATURE_TOLERANCE` | lead-api | `5m` | How far `X-Timestamp` may be from the server clock |
| `SYNC_INTERVAL` | sheets-sync | `2s` | How often to look for leads to sync |
| `SYNC_BATCH_SIZE` | sheets-sync | `50` | Leads written per pass, 1 to 500 |
| `LOG_LEVEL` | both | `info` | `debug`, `info`, `warn` or `error` |

Each service checks its settings at startup and lists every problem in one error.

## Project layout

```
services/
  lead-api/          main.go, Dockerfile, api/ (HTTP handlers and signature check)
  sheets-sync/       main.go, Dockerfile
internal/            libraries shared by both services
  config/            per-service settings from the environment
  lead/              lead rules: upsert decision, stage transitions (no I/O)
  store/             Postgres access, migrations, upsert and stage transactions
  sheets/            Google sign-in, token encryption, sync worker
  testdb/            Postgres container for tests
db/
  migrations/        schema (goose), embedded and applied by whichever service starts first
  queries/           SQL for sqlc. Run make generate after editing.
scripts/             setup.sh, send-lead.sh
docs/                requirements and design
compose.yaml         db + both services
.claude/             instructions and work journal for Claude Code
```
