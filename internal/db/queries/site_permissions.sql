-- name: ListSitePermissions :many
SELECT * FROM site_permissions WHERE site_id = $1 ORDER BY id;

-- name: UpsertSiteUserPermission :one
INSERT INTO site_permissions (
    site_id, user_id,
    perm_read, perm_execute, perm_write,
    perm_logs, perm_terminal, perm_inspect
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (site_id, user_id) WHERE user_id IS NOT NULL
DO UPDATE SET
    perm_read     = EXCLUDED.perm_read,
    perm_execute  = EXCLUDED.perm_execute,
    perm_write    = EXCLUDED.perm_write,
    perm_logs     = EXCLUDED.perm_logs,
    perm_terminal = EXCLUDED.perm_terminal,
    perm_inspect  = EXCLUDED.perm_inspect,
    updated_at    = now()
RETURNING *;

-- name: UpsertSiteTeamPermission :one
INSERT INTO site_permissions (
    site_id, team_id,
    perm_read, perm_execute, perm_write,
    perm_logs, perm_terminal, perm_inspect
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (site_id, team_id) WHERE team_id IS NOT NULL
DO UPDATE SET
    perm_read     = EXCLUDED.perm_read,
    perm_execute  = EXCLUDED.perm_execute,
    perm_write    = EXCLUDED.perm_write,
    perm_logs     = EXCLUDED.perm_logs,
    perm_terminal = EXCLUDED.perm_terminal,
    perm_inspect  = EXCLUDED.perm_inspect,
    updated_at    = now()
RETURNING *;

-- name: DeleteSitePermission :exec
DELETE FROM site_permissions WHERE id = $1 AND site_id = $2;

-- name: SitePermissionsForUser :one
-- Returns the union of direct user perms + team-mediated perms a
-- user holds for a site. Aggregated (OR-ed) at the SQL layer.
-- BOOL_OR over zero rows yields NULL — COALESCE to false so the
-- "no permissions" case comes back as an all-false row.
SELECT
    COALESCE(BOOL_OR(perm_read),     false)::boolean AS perm_read,
    COALESCE(BOOL_OR(perm_execute),  false)::boolean AS perm_execute,
    COALESCE(BOOL_OR(perm_write),    false)::boolean AS perm_write,
    COALESCE(BOOL_OR(perm_logs),     false)::boolean AS perm_logs,
    COALESCE(BOOL_OR(perm_terminal), false)::boolean AS perm_terminal,
    COALESCE(BOOL_OR(perm_inspect),  false)::boolean AS perm_inspect
  FROM site_permissions sp
 WHERE sp.site_id = $1
   AND (
        sp.user_id = $2
        OR sp.team_id IN (SELECT team_id FROM team_members WHERE user_id = $2)
   );
