-- +goose Up

CREATE TABLE teams (
    id          BIGSERIAL    PRIMARY KEY,
    name        TEXT         NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    description TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE team_members (
    team_id      BIGINT       NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id      BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_in_team TEXT         NOT NULL
        CHECK (role_in_team IN ('owner', 'member'))
        DEFAULT 'member',
    added_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_members_user_idx ON team_members (user_id);

CREATE TABLE site_permissions (
    id          BIGSERIAL    PRIMARY KEY,
    site_id     BIGINT       NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    -- Exactly one of (user_id, team_id) must be set. Enforced by CHECK.
    user_id     BIGINT       REFERENCES users(id) ON DELETE CASCADE,
    team_id     BIGINT       REFERENCES teams(id) ON DELETE CASCADE,
    perm_read     BOOLEAN    NOT NULL DEFAULT false,
    perm_execute  BOOLEAN    NOT NULL DEFAULT false,
    perm_write    BOOLEAN    NOT NULL DEFAULT false,
    perm_logs     BOOLEAN    NOT NULL DEFAULT false,
    perm_terminal BOOLEAN    NOT NULL DEFAULT false,
    perm_inspect  BOOLEAN    NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CHECK ((user_id IS NULL) <> (team_id IS NULL))
);

CREATE UNIQUE INDEX site_permissions_user_uniq
    ON site_permissions (site_id, user_id)
    WHERE user_id IS NOT NULL;

CREATE UNIQUE INDEX site_permissions_team_uniq
    ON site_permissions (site_id, team_id)
    WHERE team_id IS NOT NULL;

-- +goose Down
DROP TABLE site_permissions;
DROP TABLE team_members;
DROP TABLE teams;
