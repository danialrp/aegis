-- +goose Up

-- One deploy script per site (the "current" script). Updates rotate
-- the previous body into site_deploy_script_versions for history.
CREATE TABLE site_deploy_scripts (
    site_id      BIGINT       PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
    body         TEXT         NOT NULL DEFAULT '',
    cron_spec    TEXT,        -- Phase 1.7; NULL means no schedule
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Append-only audit trail of every save. Latest is also visible in
-- site_deploy_scripts; here it's just historical.
CREATE TABLE site_deploy_script_versions (
    id         BIGSERIAL    PRIMARY KEY,
    site_id    BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    body       TEXT         NOT NULL,
    saved_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    saved_by   BIGINT       REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX site_deploy_script_versions_site_idx
    ON site_deploy_script_versions (site_id, saved_at DESC);

-- +goose Down
DROP TABLE site_deploy_script_versions;
DROP TABLE site_deploy_scripts;
