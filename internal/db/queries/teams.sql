-- name: CreateTeam :one
INSERT INTO teams (name, description)
VALUES ($1, $2)
RETURNING *;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = $1;

-- name: ListTeams :many
SELECT * FROM teams ORDER BY id;

-- name: UpdateTeam :one
UPDATE teams
   SET name        = $2,
       description = $3,
       updated_at  = now()
 WHERE id = $1
RETURNING *;

-- name: DeleteTeam :exec
DELETE FROM teams WHERE id = $1;

-- name: AddTeamMember :exec
INSERT INTO team_members (team_id, user_id, role_in_team)
VALUES ($1, $2, $3)
ON CONFLICT (team_id, user_id) DO UPDATE
    SET role_in_team = EXCLUDED.role_in_team;

-- name: RemoveTeamMember :exec
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: ListTeamMembers :many
SELECT tm.team_id, tm.user_id, tm.role_in_team, tm.added_at,
       u.email, u.role
  FROM team_members tm
  JOIN users u ON u.id = tm.user_id
 WHERE tm.team_id = $1
 ORDER BY tm.added_at;

-- name: ListTeamsForUser :many
SELECT t.*
  FROM teams t
  JOIN team_members tm ON tm.team_id = t.id
 WHERE tm.user_id = $1
 ORDER BY t.id;
