// Server-domain API client. Mirrors the response shapes in
// internal/api/v1/servers.go. Keep this file in lockstep with the
// backend handlers — there is no codegen yet.
import { apiFetch } from '@/lib/api'

export type ServerStatus = 'pending' | 'provisioning' | 'ready' | 'error'

export interface Server {
  id: number
  name: string
  public_ip: string
  ssh_user: string
  status: ServerStatus
  agent_fingerprint?: string | null
  agent_last_seen?: string | null
  provision_error?: string | null
  created_at: string
  updated_at: string
}

export interface CreateServerInput {
  name: string
  public_ip: string
  ssh_user: string
  ssh_port: number
  ssh_password?: string
  ssh_private_key?: string
}

export function listServers(): Promise<Server[]> {
  return apiFetch<Server[]>('/v1/servers')
}

export function getServer(id: number): Promise<Server> {
  return apiFetch<Server>(`/v1/servers/${id}`)
}

export function createServer(input: CreateServerInput): Promise<Server> {
  return apiFetch<Server>('/v1/servers', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function deleteServer(id: number): Promise<void> {
  return apiFetch<void>(`/v1/servers/${id}`, { method: 'DELETE' })
}
