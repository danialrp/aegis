-- name: CreateServer :one
INSERT INTO servers (name, public_ip, ssh_user, provision_status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetServer :one
SELECT * FROM servers WHERE id = $1;

-- name: ListServers :many
SELECT * FROM servers ORDER BY id;

-- name: SetServerAgentFingerprint :exec
UPDATE servers
   SET agent_fingerprint = $2,
       updated_at        = now()
 WHERE id = $1;

-- name: TouchServerAgent :exec
UPDATE servers
   SET agent_last_seen = now(),
       updated_at      = now()
 WHERE id = $1;

-- name: SetServerStatus :exec
UPDATE servers
   SET provision_status = $2,
       updated_at       = now()
 WHERE id = $1;

-- name: SetServerProvisionError :exec
UPDATE servers
   SET provision_status = 'error',
       provision_error  = $2,
       updated_at       = now()
 WHERE id = $1;

-- name: ClearServerProvisionError :exec
UPDATE servers
   SET provision_error = NULL,
       updated_at      = now()
 WHERE id = $1;

-- name: DeleteServer :exec
DELETE FROM servers WHERE id = $1;
