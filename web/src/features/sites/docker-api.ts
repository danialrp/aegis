// Docker compose API client. Mirrors internal/api/v1/docker.go.
import { apiFetch } from '@/lib/api'

export type ComposeAction = 'up' | 'down' | 'restart' | 'pull' | 'build'

export interface Compose {
  site_id: number
  body: string
  updated_at: string
}

export interface Container {
  name: string
  service: string
  image: string
  state: string
  status: string
}

export function getCompose(siteID: number): Promise<Compose> {
  return apiFetch<Compose>(`/v1/sites/${siteID}/compose`)
}

export function putCompose(siteID: number, body: string): Promise<Compose> {
  return apiFetch<Compose>(`/v1/sites/${siteID}/compose`, {
    method: 'PUT',
    body: JSON.stringify({ body }),
  })
}

export function composeAction(
  siteID: number,
  action: ComposeAction
): Promise<void> {
  return apiFetch<void>(`/v1/sites/${siteID}/compose/action`, {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export function listContainers(siteID: number): Promise<Container[]> {
  return apiFetch<Container[]>(`/v1/sites/${siteID}/containers`)
}

export function getContainerLogs(
  siteID: number,
  service: string
): Promise<{ output: string }> {
  return apiFetch<{ output: string }>(
    `/v1/sites/${siteID}/containers/${encodeURIComponent(service)}/logs`
  )
}
