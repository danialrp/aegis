-- name: GetSiteCompose :one
SELECT * FROM site_compose WHERE site_id = $1;

-- name: UpsertSiteCompose :one
INSERT INTO site_compose (site_id, body)
VALUES ($1, $2)
ON CONFLICT (site_id)
DO UPDATE SET
    body       = EXCLUDED.body,
    updated_at = now()
RETURNING *;
