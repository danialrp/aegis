-- +goose Up
CREATE TABLE sites (
    id               BIGSERIAL    PRIMARY KEY,
    server_id        BIGINT       NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name             TEXT         NOT NULL,
    domain           TEXT         NOT NULL,
    site_type        TEXT         NOT NULL
        CHECK (site_type IN ('static', 'php', 'docker')),
    provision_status TEXT         NOT NULL
        CHECK (provision_status IN ('pending', 'provisioning', 'ready', 'error'))
        DEFAULT 'pending',
    provision_error  TEXT,
    working_dir      TEXT         NOT NULL,  -- /srv/sites/<id> by convention
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (server_id, domain)
);

CREATE INDEX sites_server_id_idx        ON sites (server_id);
CREATE INDEX sites_provision_status_idx ON sites (provision_status);

-- +goose Down
DROP TABLE sites;
