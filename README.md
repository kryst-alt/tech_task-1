# Lead Management Service

Receives leads from a partner website, checks they are authentic, stores each lead once in Postgres, and keeps a Google Sheet in sync.

Written in Go with Fiber and Postgres. Design notes are in [docs/STACK.md](docs/STACK.md), and the requirements are in [docs/PROJECT.md](docs/PROJECT.md).

## Status

| Feature | State |
|---|---|
| Signed lead endpoint with duplicate and email handling | Done |
| Tests for invalid signature, duplicates and email update | Done |
| Deal stages in the database | Done |
| Endpoint to change a lead's stage | Done |
| Google sign-in and Sheets sync | Done |

## Requirements

- Go 1.26 or newer
- Podman with `podman-compose`, or Docker with Compose
- `openssl` and `curl`, for `scripts/send-lead.sh`

On Fedora:

```sh
sudo dnf install golang podman-compose
systemctl --user enable --now podman.socket   # the tests reach Podman through this socket
```

## Quick start

```sh
cp .env.example .env
```

Fill in `.env`. `LEAD_SIGNING_SECRET`, `ADMIN_API_KEY` and `TOKEN_ENCRYPTION_KEY` have the commands to generate them in the comments. For the `GOOGLE_*` settings, see [Google Sheets setup](#google-sheets-setup).

```sh
make db-up    # Postgres on localhost:5432
make run      # API on localhost:8080, applies migrations on start
```

In a second terminal:

```sh
scripts/send-lead.sh '{"id":"p-1","name":"Ada","email":"ada@example.com","company":"Acme","source":"partner"}'
```

The first send returns `201 {"result":"create"}`. Sending it again returns `200 {"result":"unchanged"}`.

Stop the server with Ctrl+C, then `make db-down`. Data stays in a volume. `podman compose down -v` deletes it.

## Google Sheets setup

Done once per deployment.

1. In the [Google Cloud console](https://console.cloud.google.com/), create a project and enable the **Google Sheets API** and **Google Drive API**.
2. Under **OAuth consent screen**, choose External and add the Google account that should own the sheet under **Test users**.
3. Create **Credentials > OAuth client ID > Web application** with the redirect URI `http://localhost:8080/auth/google/callback`.
4. Copy the client ID and secret into `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `.env`. The downloaded `client_secret_*.json` is git-ignored. Don't commit it.
5. Start the server and open `http://localhost:8080/auth/google?key=<ADMIN_API_KEY>` in a browser on the same machine. Sign in with the test user account.

The app creates a spreadsheet called **Leads** in that account's Drive and shows its link. Leads already in the database sync within a few seconds. `GET /healthz` reports `"google":"ok"`.

The server stores the refresh token encrypted and renews the hourly access token by itself, so nobody signs in again after a restart or the next day. Two cases do need a new sign-in:

- Google revokes access, for example after a password change. Sync pauses, and `/healthz` reports `"google":"reauth_required"`. Open the sign-in link again.
- The consent screen is still in **Testing** status. Google expires refresh tokens after 7 days in that state. Set it to **In production** for anything longer than a demo.

## Tests

```sh
make test
```

The tests start their own Postgres container, so `make db-up` is not needed. The Makefile points testcontainers at the Podman socket. With Docker, run `DOCKER_HOST= make test`.

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

| Variable | Purpose |
|---|---|
| `PORT` | HTTP port, default 8080 |
| `DATABASE_URL` | Postgres connection string |
| `LEAD_SIGNING_SECRET` | Shared HMAC secret, at least 32 characters |
| `ADMIN_API_KEY` | Bearer token for admin endpoints, at least 32 characters |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` | Google OAuth client |
| `TOKEN_ENCRYPTION_KEY` | 32 bytes, base64, encrypts the stored Google refresh token |

The server checks all of them at startup and lists every problem in one error.

## Project layout

```
cmd/server/          entry point
internal/api/        HTTP handlers and signature check
internal/lead/       lead rules: upsert decision, stage transitions (no I/O)
internal/store/      Postgres access, migrations, the upsert transaction
internal/sheets/     Google sign-in, token encryption, sync worker
internal/testdb/     Postgres container for tests
db/migrations/       schema (goose), embedded in the binary
db/queries/          SQL for sqlc. Run `make generate` after editing.
scripts/             developer tools
docs/                requirements and design
.claude/             instructions and work journal for Claude Code
```
