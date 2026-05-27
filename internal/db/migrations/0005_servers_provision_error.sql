-- +goose Up
ALTER TABLE servers ADD COLUMN provision_error TEXT;

-- +goose Down
ALTER TABLE servers DROP COLUMN provision_error;
