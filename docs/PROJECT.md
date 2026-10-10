# Project: Lead Management and Automation System

A partner website sends us new leads from people interested in our services. We need a system that receives those leads securely, prevents duplicates, stores them in a database and keeps them in sync with Google Sheets.

Stack and approach: `docs/STACK.md`.

## Requirements

### R1. Receiving leads securely

| ID | Requirement |
|---|---|
| R1.1 | An API endpoint accepts a new lead with an `id`, `name`, `email`, `company` and `source`. |
| R1.2 | Each request carries a secret signature. Requests with a missing or invalid signature are rejected. |
| R1.3 | The same lead is never saved twice. |
| R1.4 | A lead whose email already exists updates the existing record instead of creating a new one. |

### R2. Database

| ID | Requirement |
|---|---|
| R2.1 | Leads are stored in a free database of our choice. |
| R2.2 | Each lead has a deal stage: `New` → `Qualified` → `Proposal` → `Won` or `Lost`. |

### R3. Google Sheets sync

| ID | Requirement |
|---|---|
| R3.1 | Lead records sync to Google Sheets automatically whenever a lead is added or updated. |
| R3.2 | The integration uses Google OAuth ("Sign in with Google"), not a service account. |
| R3.3 | The integration still works the next day without the user signing in again. |

### R4. Tests

| ID | The tests must show that |
|---|---|
| R4.1 | Leads with an invalid signature are rejected. |
| R4.2 | Duplicate submissions don't create duplicate records. |
| R4.3 | An incoming lead with an existing email updates the existing record. |

## Open questions

`docs/STACK.md` sets a default for each of these. The user can override any of them.

1. **Signature scheme.** Probably an HMAC-SHA256 of the raw request body in a header. Also: which header name, should the signature cover a timestamp to stop replayed requests, and how will the partner get the secret?
2. **What counts as a duplicate.** R1.3 most naturally means the same lead `id`, and R1.4 means the same `email`. What happens when a known `id` arrives with a different email, or a new `id` arrives with an existing email but different fields?
3. **Deal stage rules.** New leads start at `New`. Can a stage go backwards? Can `Won` and `Lost` be changed once set? Does an incoming update leave the stage alone?
4. **When to sync.** Does the Sheets sync happen inside the request, or in the background with retries? What happens to the lead if Google is down?
5. **Sheet layout.** One row per lead, found by `id` or `email`? Which columns, and which spreadsheet and tab?
6. **Token storage.** Where is the refresh token kept (database or encrypted file), and what happens when Google revokes it?
7. **Database and deployment.** Which free database, and does this need to run anywhere besides locally?

## Definition of done

- Every requirement R1 to R4 is met and checked against the running app.
- The R4 tests pass from a single command.
- The README covers setup: environment variables, Google OAuth client setup, the one-time sign-in, and how to send a signed test lead.
- No secrets are in the repo.
