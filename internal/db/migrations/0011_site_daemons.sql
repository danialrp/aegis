-- +goose Up
CREATE TABLE site_daemons (
    id               BIGSERIAL    PRIMARY KEY,
    site_id          BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    slug             TEXT         NOT NULL
        CHECK (slug ~ '^[a-z0-9-]+$'),
    name             TEXT         NOT NULL,
    command          TEXT         NOT NULL,
    auto_restart     BOOLEAN      NOT NULL DEFAULT true,
    status           TEXT         NOT NULL DEFAULT 'unknown',
    last_action_at   TIMESTAMPTZ,
    last_error       TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (site_id, slug)
);

CREATE INDEX site_daemons_site_idx ON site_daemons (site_id);

-- +goose Down
DROP TABLE site_daemons;
