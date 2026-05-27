-- name: CreateSiteDaemon :one
INSERT INTO site_daemons (site_id, slug, name, command, auto_restart)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSiteDaemon :one
SELECT * FROM site_daemons WHERE id = $1;

-- name: ListSiteDaemonsForSite :many
SELECT * FROM site_daemons WHERE site_id = $1 ORDER BY id DESC;

-- name: UpdateSiteDaemonStatus :exec
UPDATE site_daemons
   SET status         = $2,
       last_action_at = now(),
       last_error     = $3,
       updated_at     = now()
 WHERE id = $1;

-- name: DeleteSiteDaemon :exec
DELETE FROM site_daemons WHERE id = $1;
