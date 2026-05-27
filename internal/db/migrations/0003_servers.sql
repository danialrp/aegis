-- +goose Up
CREATE TABLE servers (
    id                 BIGSERIAL    PRIMARY KEY,
    name               TEXT         NOT NULL,
    public_ip          INET         NOT NULL,
    ssh_user           TEXT         NOT NULL,
    agent_fingerprint  TEXT         UNIQUE,
    agent_last_seen    TIMESTAMPTZ,
    provision_status   TEXT         NOT NULL
        CHECK (provision_status IN ('pending', 'provisioning', 'ready', 'error')),
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX servers_provision_status_idx ON servers (provision_status);

-- +goose Down
DROP TABLE servers;
