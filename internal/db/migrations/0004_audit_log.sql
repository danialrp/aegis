-- +goose Up
CREATE TABLE audit_log (
    id              BIGSERIAL    PRIMARY KEY,
    actor_user_id   BIGINT       REFERENCES users(id) ON DELETE SET NULL,
    actor_ip        INET,
    action          TEXT         NOT NULL,
    target_type     TEXT,
    target_id       BIGINT,
    payload         JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_actor_user_id_idx ON audit_log (actor_user_id);
CREATE INDEX audit_log_target_idx        ON audit_log (target_type, target_id);
CREATE INDEX audit_log_created_at_idx    ON audit_log (created_at DESC);

-- +goose Down
DROP TABLE audit_log;
