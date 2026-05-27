-- +goose Up
-- One compose file per docker-type site. Stored in DB + mirrored to
-- /srv/sites/<id>/compose.yml on save so `docker compose` finds it.
CREATE TABLE site_compose (
    site_id      BIGINT       PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
    body         TEXT         NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE site_compose;
