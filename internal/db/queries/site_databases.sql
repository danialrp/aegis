-- name: CreateSiteDatabase :one
INSERT INTO site_databases (site_id, engine, name, username, password)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSiteDatabase :one
SELECT * FROM site_databases WHERE id = $1;

-- name: ListSiteDatabasesForSite :many
SELECT * FROM site_databases WHERE site_id = $1 ORDER BY id DESC;

-- name: SetSiteDatabaseStatus :exec
UPDATE site_databases
   SET status     = $2,
       last_error = $3,
       updated_at = now()
 WHERE id = $1;

-- name: TouchSiteDatabaseBackup :exec
UPDATE site_databases
   SET last_backup_at = now(),
       updated_at     = now()
 WHERE id = $1;

-- name: DeleteSiteDatabase :exec
DELETE FROM site_databases WHERE id = $1;
