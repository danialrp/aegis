-- +goose Up
ALTER TABLE sites ADD COLUMN webhook_secret TEXT;

-- +goose Down
ALTER TABLE sites DROP COLUMN webhook_secret;
