-- +goose Up
CREATE TABLE deploys (
    id           BIGSERIAL    PRIMARY KEY,
    site_id      BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    trigger      TEXT         NOT NULL
        CHECK (trigger IN ('manual', 'webhook', 'schedule')),
    status       TEXT         NOT NULL
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed'))
        DEFAULT 'queued',
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    exit_code    INTEGER,
    output_log   TEXT         NOT NULL DEFAULT '',
    triggered_by BIGINT       REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX deploys_site_idx        ON deploys (site_id, created_at DESC);
CREATE INDEX deploys_status_idx      ON deploys (status);

-- +goose Down
DROP TABLE deploys;
