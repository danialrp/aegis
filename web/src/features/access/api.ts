// Access-domain API client: users + teams + per-site permissions.
import { apiFetch } from '@/lib/api'

export type UserRole = 'god' | 'admin' | 'site_user'

export interface User {
  id: number
  email: string
  role: UserRole
  enabled: boolean
  created_at: string
}

export interface CreateUserInput {
  email: string
  password: string
  role: UserRole
  enabled: boolean
}

export interface Team {
  id: number
  name: string
  description?: string | null
  created_at: string
}

export interface TeamMember {
  team_id: number
  user_id: number
  email: string
  role_in_team: 'owner' | 'member'
  user_role: UserRole
  added_at: string
}

export interface SitePermissionFlags {
  read: boolean
  execute: boolean
  write: boolean
  logs: boolean
  terminal: boolean
  inspect: boolean
}

export interface SitePermission extends SitePermissionFlags {
  id: number
  site_id: number
  user_id?: number | null
  team_id?: number | null
}

// --- users ---
export const listUsers = () => apiFetch<User[]>('/v1/users')
export const createUser = (input: CreateUserInput) =>
  apiFetch<User>('/v1/users', { method: 'POST', body: JSON.stringify(input) })
export const deleteUser = (id: number) =>
  apiFetch<void>(`/v1/users/${id}`, { method: 'DELETE' })
export const updateUser = (
  id: number,
  patch: { enabled?: boolean; password?: string }
) =>
  apiFetch<User>(`/v1/users/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  })

// --- teams ---
export const listTeams = () => apiFetch<Team[]>('/v1/teams')
export const createTeam = (name: string, description?: string) =>
  apiFetch<Team>('/v1/teams', {
    method: 'POST',
    body: JSON.stringify({ name, description }),
  })
export const deleteTeam = (id: number) =>
  apiFetch<void>(`/v1/teams/${id}`, { method: 'DELETE' })
export const listTeamMembers = (teamID: number) =>
  apiFetch<TeamMember[]>(`/v1/teams/${teamID}/members`)
export const addTeamMember = (
  teamID: number,
  userID: number,
  role: 'owner' | 'member' = 'member'
) =>
  apiFetch<void>(`/v1/teams/${teamID}/members`, {
    method: 'POST',
    body: JSON.stringify({ user_id: userID, role }),
  })
export const removeTeamMember = (teamID: number, userID: number) =>
  apiFetch<void>(`/v1/teams/${teamID}/members/${userID}`, {
    method: 'DELETE',
  })

// --- site permissions ---
export const listSitePermissions = (siteID: number) =>
  apiFetch<SitePermission[]>(`/v1/sites/${siteID}/permissions`)
export const upsertSitePermission = (
  siteID: number,
  body: { user_id?: number; team_id?: number } & SitePermissionFlags
) =>
  apiFetch<SitePermission>(`/v1/sites/${siteID}/permissions`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
export const deleteSitePermission = (siteID: number, permID: number) =>
  apiFetch<void>(`/v1/sites/${siteID}/permissions/${permID}`, {
    method: 'DELETE',
  })
