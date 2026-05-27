-- +goose Up
CREATE TABLE users (
    id            BIGSERIAL    PRIMARY KEY,
    email         TEXT         UNIQUE NOT NULL,
    password_hash TEXT         NOT NULL,
    role          TEXT         NOT NULL CHECK (role IN ('god', 'admin', 'site_user')),
    enabled       BOOLEAN      NOT NULL DEFAULT false,
    mfa_secret    TEXT,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX users_role_idx ON users (role);

-- +goose Down
DROP TABLE users;
