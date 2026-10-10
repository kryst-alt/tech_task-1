-- +goose Up
ALTER TABLE google_credentials ADD COLUMN needs_reauth boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE google_credentials DROP COLUMN needs_reauth;
