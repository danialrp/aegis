-- name: CreateUser :one
INSERT INTO users (email, password_hash, role, enabled)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY id;

-- name: UpdateUserPassword :exec
UPDATE users
   SET password_hash = $2,
       updated_at    = now()
 WHERE id = $1;

-- name: SetUserEnabled :exec
UPDATE users
   SET enabled    = $2,
       updated_at = now()
 WHERE id = $1;

-- name: SetUserMFASecret :exec
UPDATE users
   SET mfa_secret = $2,
       updated_at = now()
 WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
