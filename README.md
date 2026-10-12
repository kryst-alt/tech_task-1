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

## Why this approach

- **Go with Fiber, Postgres and sqlc.** Each service builds to one small binary, and Go's goroutines suit a background worker. Postgres enforces the duplicate rules itself: unique indexes on the partner's `id` and on `email`, row locks, and `citext` so emails match regardless of case. sqlc turns plain SQL into typed Go functions, so a wrong column name fails at build time without an ORM in between.
- **Two services, not one and not five.** lead-api answers the partner without waiting for Google. sheets-sync talks to Google on its own schedule. They share the database and never call each other, so either one can be down without losing a lead. More services would add network calls and deployment work without solving a problem this system has.
- **The database is the sync queue.** Every change to a lead raises `leads.version`, and the worker copies any lead whose version is ahead of `lead_sync.synced_version`. A message broker would be a second system to run and keep consistent with the database. With versions, ten quick edits to one lead become one sheet write, and a Google outage only delays syncing.
- **HMAC-SHA256 over the timestamp and the raw body.** This is the scheme Stripe uses to sign webhooks, so a partner developer will likely recognise it. Signing the raw bytes means the check never depends on how the JSON gets parsed. The timestamp, with a 5-minute window, stops anyone replaying a recorded request, and the comparison runs in constant time.
- **Duplicates are matched by id, then by email, inside one transaction.** Both candidate rows are locked with `SELECT ... FOR UPDATE`. If the id belongs to one stored lead and the email to another, the request gets 409 instead of silently merging two people. When two first submissions arrive at the same moment, the unique index rejects one, and it retries once as an update. A test forces that race.
- **Incoming leads never change the deal stage.** The partner owns the contact details and the sales team owns the stage. Stages only move forward through `PATCH /v1/leads/{id}/stage`, and Won and Lost are final.
- **OAuth with offline access and an encrypted refresh token.** One admin signs in once. Google returns a refresh token, which the app stores encrypted with AES-256-GCM, and the worker renews the hourly access token by itself. The `drive.file` scope limits the app to the spreadsheet it created. If Google revokes access, syncing pauses, `/healthz` reports `reauth_required`, and queued leads wait for a new sign-in.
- **Tests run against a real Postgres.** testcontainers starts a throwaway database for each test run. Duplicate handling depends on constraints and locks, and a mocked database could not prove those work.

## Requirements

- Docker Engine with the Compose plugin (Podman with `podman-compose` also works)
- `openssl`, `curl`, and `python3` (setup reads the Google JSON with it)
- Go 1.26 or newer, only for running tests or the services outside containers

Docker on Fedora (other systems: <https://docs.docker.com/engine/install/>):

```sh
sudo dnf config-manager addrepo --from-repofile=https://download.docker.com/linux/fedora/docker-ce.repo
sudo dnf install golang docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
sudo usermod -aG docker $USER   # then log out and back in, so docker runs without sudo
```

The Makefile uses Docker when it is installed. Without Docker it falls back to Podman; then also run `systemctl --user enable --now podman.socket` so `make test` can reach it.

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

`make logs` follows both services. `make down` stops them and keeps the data. `docker compose down -v` deletes the data too.

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
make check     # Go tests, then acceptance tests against the running stack, with a pass/fail report
make test      # Go tests only, plain go test output
```

There are three layers. Each checks something the others can't.

| Layer | Where | Runs against | Proves |
|---|---|---|---|
| Go unit and integration tests | `*_test.go` | A throwaway Postgres container; Google is faked | Logic, SQL, locking and the sync worker, including races and failures that are hard to trigger from outside |
| Acceptance tests (pytest) | `tests/acceptance/` | The running containers and the real Google Sheet | The deployed system end to end: HTTP responses, what Postgres stored, and that sheets-sync actually wrote each change |
| Postman collection | `postman/` | The running containers | The same API by hand, for demos and exploring |

The acceptance tests are in Python on purpose. They are black-box tests owned by QA in many teams, they use only HTTP and read-only SQL, and they never import the Go code, so they can't share its mistakes. `scripts/test.sh` creates their virtualenv on the first run.

Run one suite or one area:

```sh
scripts/test.sh go                    # Go tests only, readable output (gotestsum)
scripts/test.sh acceptance            # acceptance tests only (needs make up)
scripts/test.sh signature             # one area in both suites
scripts/test.sh acceptance stages     # one area in one suite
```

Areas: `signature`, `duplicates`, `email-update`, `stages`, `sync`, `health`, `config`. The Go tests start their own Postgres container, so `make db-up` is not needed. They use Docker, or the Podman socket when Docker is not installed. The acceptance sync tests write rows to the linked sheet; their lead IDs start with `qa-`.

The Go tests the brief asks for:

| Test | Proves |
|---|---|
| `TestLeadWithInvalidSignatureIsRejected` | A missing, wrong or stale signature returns 401 and saves nothing |
| `TestDuplicateSubmissionCreatesOneRecord` | Sending the same lead twice stores one row |
| `TestLeadWithExistingEmailUpdatesRecord` | A new ID with a known email updates that lead |
| `TestUpsertLosingInsertRaceBecomesUpdate` | Two simultaneous first submissions still store one row |
| `internal/sheets` tests | The worker writes each change once, retries with backoff, waits while unlinked, and pauses on a revoked token |

## Demo with Postman

[`postman/`](postman) holds a collection that signs each request the way the partner does and checks every response.

1. Run `scripts/postman-env.sh`. It writes `postman/local.postman_environment.json` with the secrets from `.env`. Git ignores that file.
2. In Postman, import `postman/lead-api.postman_collection.json` and the local environment, then select **Lead API (local, with secrets)**.
3. Run the collection. Every run uses new lead IDs, so it can be repeated.

| Folder | Checks |
|---|---|
| 0. Health | Database reachable, Google linked |
| 1. Receive leads | Create (201), same lead again (200 unchanged), update by ID, update by existing email, ID and email of different leads (409), invalid lead (422) |
| 2. Signature check | Missing signature, wrong secret, body changed after signing, 10-minute-old timestamp: all 401 |
| 3. Deal stages | Allowed moves (200), skipped stage and change after Won (409), unknown stage (422), unknown lead (404), no admin key (401) |

Without Postman, the same 21 requests and 32 checks run from the terminal:

```sh
npx newman run postman/lead-api.postman_collection.json -e postman/local.postman_environment.json
```

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

## Running on a home server

The stack can run on an always-on machine and be used from other devices. Compose restarts the containers after a reboot, and Postgres only listens on the server itself. Port 8080 is open to the network; every endpoint still needs the HMAC signature or the admin key.

A private overlay network such as ZeroTier or Tailscale makes the server reachable from anywhere without opening router ports. Replace `<server-ip>` below with the server's address on that network.

| Task | From another device |
|---|---|
| Call the API, use Postman | `http://<server-ip>:8080`. Generate the Postman environment on the server with `scripts/postman-env.sh http://<server-ip>:8080` and copy it over. |
| Link or re-link Google | `ssh -L 8080:localhost:8080 <user>@<server-ip>`, then open `http://localhost:8080/auth/google?key=<ADMIN_API_KEY>` in the local browser. Google accepts only `localhost` or an HTTPS public domain as a redirect, so the tunnel makes the callback land on the server. |
| Run the tests | `ssh <user>@<server-ip>`, then `make check` in the project folder. The acceptance tests read Postgres, which only the server can reach. |

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
| `DB_PORT` | compose | `5432` | Postgres port on the host, reachable only from this machine |
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
scripts/             setup.sh, send-lead.sh, postman-env.sh, test.sh
tests/acceptance/    pytest suite that tests the running system from outside
postman/             Postman collection and environment template
docs/                requirements and design
compose.yaml         db + both services
.claude/             instructions and work journal for Claude Code
```

## What I'd improve with more time

1. **Team access with Google sign-in.** Today one admin key protects stage changes and the Google link. Next would be a `users` table of invited emails with roles: admins invite people and manage the sheet, members move deals. People would sign in with Google, get a session stored in the database, and send a CSRF token with every change. The partner endpoint keeps its HMAC check, since lead intake does not involve users.
2. **Keep the admin key out of URLs.** `/auth/google?key=...` leaves the admin key in browser history. Team sign-in would replace it. Until then, a one-time link that expires after 5 minutes would close the gap.
3. **Rate limiting and request logs** on `POST /v1/leads`, so a misbehaving client can't flood the service and every rejected request can be traced.
4. **Fewer Google API calls.** Each lead costs two calls: read column A to find the row, then write it. Reading column A once per batch and sending one batch update would keep large imports under Google's per-minute quota.
5. **Signing-secret rotation.** Accept two secrets during a rollover so the partner can switch without failed requests.
6. **Save rotated refresh tokens.** If Google returns a new refresh token while renewing access, the app does not store it yet.
7. **Hosting and monitoring.** Run both images behind HTTPS, track sync lag and failed attempts, and alert when `/healthz` reports `reauth_required`.
