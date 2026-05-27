-- name: CreateSite :one
INSERT INTO sites (server_id, name, domain, site_type, working_dir, provision_status)
VALUES ($1, $2, $3, $4, $5, 'pending')
RETURNING *;

-- name: GetSite :one
SELECT * FROM sites WHERE id = $1;

-- name: ListSites :many
SELECT * FROM sites ORDER BY id DESC;

-- name: ListSitesByServer :many
SELECT * FROM sites WHERE server_id = $1 ORDER BY id DESC;

-- name: SetSiteStatus :exec
UPDATE sites
   SET provision_status = $2,
       updated_at       = now()
 WHERE id = $1;

-- name: SetSiteWorkingDir :exec
UPDATE sites
   SET working_dir = $2,
       updated_at  = now()
 WHERE id = $1;

-- name: SetSiteProvisionError :exec
UPDATE sites
   SET provision_status = 'error',
       provision_error  = $2,
       updated_at       = now()
 WHERE id = $1;

-- name: ClearSiteProvisionError :exec
UPDATE sites
   SET provision_error = NULL,
       updated_at      = now()
 WHERE id = $1;

-- name: DeleteSite :exec
DELETE FROM sites WHERE id = $1;

-- name: GetSiteWebhookSecret :one
SELECT webhook_secret FROM sites WHERE id = $1;

-- name: SetSiteWebhookSecret :exec
UPDATE sites
   SET webhook_secret = $2,
       updated_at     = now()
 WHERE id = $1;

-- name: SetSiteProxyPort :exec
UPDATE sites
   SET proxy_port = $2,
       updated_at = now()
 WHERE id = $1;
