// Daemons API client. Mirrors internal/api/v1/daemons.go.
import { apiFetch } from '@/lib/api'

export interface Daemon {
  id: number
  site_id: number
  slug: string
  name: string
  command: string
  auto_restart: boolean
  status: string
  last_error?: string | null
  last_action_at?: string | null
  created_at: string
}

export interface CreateDaemonInput {
  slug: string
  name: string
  command: string
  auto_restart: boolean
}

export type DaemonAction = 'start' | 'stop' | 'restart'

export function listDaemons(siteID: number): Promise<Daemon[]> {
  return apiFetch<Daemon[]>(`/v1/sites/${siteID}/daemons`)
}

export function createDaemon(
  siteID: number,
  input: CreateDaemonInput
): Promise<Daemon> {
  return apiFetch<Daemon>(`/v1/sites/${siteID}/daemons`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function daemonAction(
  siteID: number,
  daemonID: number,
  action: DaemonAction
): Promise<Daemon> {
  return apiFetch<Daemon>(`/v1/sites/${siteID}/daemons/${daemonID}/action`, {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export function getDaemonLogs(
  siteID: number,
  daemonID: number
): Promise<{ output: string }> {
  return apiFetch<{ output: string }>(
    `/v1/sites/${siteID}/daemons/${daemonID}/logs`
  )
}

export function deleteDaemon(siteID: number, daemonID: number): Promise<void> {
  return apiFetch<void>(`/v1/sites/${siteID}/daemons/${daemonID}`, {
    method: 'DELETE',
  })
}
