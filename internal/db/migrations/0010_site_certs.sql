-- +goose Up
CREATE TABLE site_certs (
    id              BIGSERIAL    PRIMARY KEY,
    site_id         BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    domain          TEXT         NOT NULL,
    status          TEXT         NOT NULL
        CHECK (status IN ('pending', 'issuing', 'active', 'expired', 'error'))
        DEFAULT 'pending',
    issued_at       TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (site_id, domain)
);

CREATE INDEX site_certs_site_idx       ON site_certs (site_id);
CREATE INDEX site_certs_expires_idx    ON site_certs (expires_at) WHERE status = 'active';

-- +goose Down
DROP TABLE site_certs;
