-- name: CreateAuditEvent :one
INSERT INTO audit_log (actor_user_id, actor_ip, action, target_type, target_id, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAuditByActor :many
SELECT *
  FROM audit_log
 WHERE actor_user_id = $1
 ORDER BY id DESC
 LIMIT $2;

-- name: ListAuditByTarget :many
SELECT *
  FROM audit_log
 WHERE target_type = $1
   AND target_id   = $2
 ORDER BY id DESC
 LIMIT $3;
