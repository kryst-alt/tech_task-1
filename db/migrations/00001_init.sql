-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TYPE lead_stage AS ENUM ('new', 'qualified', 'proposal', 'won', 'lost');

CREATE TABLE leads (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  external_id  text NOT NULL UNIQUE,
  email        citext NOT NULL UNIQUE,
  name         text NOT NULL,
  company      text NOT NULL,
  source       text NOT NULL,
  stage        lead_stage NOT NULL DEFAULT 'new',
  version      bigint NOT NULL DEFAULT 1,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lead_sync (
  lead_id          uuid PRIMARY KEY REFERENCES leads(id),
  synced_version   bigint NOT NULL DEFAULT 0,
  attempts         int NOT NULL DEFAULT 0,
  next_attempt_at  timestamptz NOT NULL DEFAULT now(),
  last_error       text
);

CREATE TABLE google_credentials (
  id                 smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  account_email      text NOT NULL,
  refresh_token_enc  bytea NOT NULL,
  spreadsheet_id     text,
  updated_at         timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE google_credentials;
DROP TABLE lead_sync;
DROP TABLE leads;
DROP TYPE lead_stage;
