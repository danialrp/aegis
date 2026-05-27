-- +goose Up
-- Phase 3: docker-type sites need an upstream port nginx proxies to.
-- NULL for static / php sites; required for docker sites at the API
-- layer (the CHECK below is intentionally loose so it doesn't block
-- existing rows).
ALTER TABLE sites ADD COLUMN proxy_port INTEGER
    CHECK (proxy_port IS NULL OR (proxy_port BETWEEN 1 AND 65535));

-- +goose Down
ALTER TABLE sites DROP COLUMN proxy_port;
