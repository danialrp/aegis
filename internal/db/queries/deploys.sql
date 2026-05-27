-- name: CreateDeploy :one
INSERT INTO deploys (site_id, trigger, triggered_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDeploy :one
SELECT * FROM deploys WHERE id = $1;

-- name: ListDeploysForSite :many
SELECT *
  FROM deploys
 WHERE site_id = $1
 ORDER BY id DESC
 LIMIT $2;

-- name: MarkDeployRunning :exec
UPDATE deploys
   SET status     = 'running',
       started_at = now()
 WHERE id = $1;

-- name: MarkDeployFinished :exec
UPDATE deploys
   SET status      = $2,
       exit_code   = $3,
       finished_at = now()
 WHERE id = $1;

-- name: AppendDeployOutput :exec
UPDATE deploys
   SET output_log = output_log || $2
 WHERE id = $1;
