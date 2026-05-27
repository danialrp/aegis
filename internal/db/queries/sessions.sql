-- name: CreateSession :one
INSERT INTO sessions (id, user_id, expires_at, ip, user_agent)
VALUES (gen_random_uuid(), $1, $2, $3, $4)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: RefreshSession :exec
UPDATE sessions
   SET refreshed_at = now(),
       expires_at   = $2
 WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < now();
