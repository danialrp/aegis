-- +goose Up
CREATE TABLE site_databases (
    id               BIGSERIAL    PRIMARY KEY,
    site_id          BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    engine           TEXT         NOT NULL CHECK (engine IN ('mysql', 'postgres')),
    name             TEXT         NOT NULL CHECK (name ~ '^[a-zA-Z0-9_]{1,63}$'),
    username         TEXT         NOT NULL CHECK (username ~ '^[a-zA-Z0-9_]{1,32}$'),
    password         TEXT         NOT NULL,  -- plaintext; encrypt-at-rest is a Phase 7+ task
    status           TEXT         NOT NULL
        CHECK (status IN ('pending', 'creating', 'ready', 'error'))
        DEFAULT 'pending',
    last_error       TEXT,
    last_backup_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (site_id, engine, name)
);

CREATE INDEX site_databases_site_idx ON site_databases (site_id);

-- +goose Down
DROP TABLE site_databases;
