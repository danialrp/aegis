-- name: CreateSiteCert :one
INSERT INTO site_certs (site_id, domain, status)
VALUES ($1, $2, 'pending')
ON CONFLICT (site_id, domain)
DO UPDATE SET
    status     = 'pending',
    last_error = NULL,
    updated_at = now()
RETURNING *;

-- name: GetSiteCert :one
SELECT * FROM site_certs WHERE id = $1;

-- name: GetSiteCertByDomain :one
SELECT * FROM site_certs WHERE site_id = $1 AND domain = $2;

-- name: ListSiteCertsForSite :many
SELECT * FROM site_certs WHERE site_id = $1 ORDER BY id DESC;

-- name: SetSiteCertStatus :exec
UPDATE site_certs
   SET status     = $2,
       updated_at = now()
 WHERE id = $1;

-- name: SetSiteCertIssued :exec
UPDATE site_certs
   SET status     = 'active',
       issued_at  = now(),
       expires_at = $2,
       last_error = NULL,
       updated_at = now()
 WHERE id = $1;

-- name: SetSiteCertError :exec
UPDATE site_certs
   SET status     = 'error',
       last_error = $2,
       updated_at = now()
 WHERE id = $1;

-- name: DeleteSiteCert :exec
DELETE FROM site_certs WHERE id = $1;
