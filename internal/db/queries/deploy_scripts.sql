-- name: GetDeployScript :one
SELECT * FROM site_deploy_scripts WHERE site_id = $1;

-- name: UpsertDeployScript :one
INSERT INTO site_deploy_scripts (site_id, body, cron_spec)
VALUES ($1, $2, $3)
ON CONFLICT (site_id)
DO UPDATE SET
    body       = EXCLUDED.body,
    cron_spec  = EXCLUDED.cron_spec,
    updated_at = now()
RETURNING *;

-- name: InsertDeployScriptVersion :exec
INSERT INTO site_deploy_script_versions (site_id, body, saved_by)
VALUES ($1, $2, $3);

-- name: ListDeployScriptVersions :many
SELECT *
  FROM site_deploy_script_versions
 WHERE site_id = $1
 ORDER BY saved_at DESC
 LIMIT $2;

-- name: ListScheduledDeployScripts :many
SELECT site_id, cron_spec
  FROM site_deploy_scripts
 WHERE cron_spec IS NOT NULL
   AND cron_spec <> ''
 ORDER BY site_id;
