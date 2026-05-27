// Deploy script + deploy history API client.
// Mirrors internal/api/v1/deploys.go.
import { apiFetch } from '@/lib/api'

export interface DeployScript {
  site_id: number
  body: string
  cron_spec?: string | null
  updated_at: string
}

export type DeployTrigger = 'manual' | 'webhook' | 'schedule'
export type DeployStatus = 'queued' | 'running' | 'succeeded' | 'failed'

export interface Deploy {
  id: number
  site_id: number
  trigger: DeployTrigger
  status: DeployStatus
  started_at?: string | null
  finished_at?: string | null
  exit_code?: number | null
  output_log: string
  created_at: string
}

export function getDeployScript(siteID: number): Promise<DeployScript> {
  return apiFetch<DeployScript>(`/v1/sites/${siteID}/deploy-script`)
}

export function putDeployScript(
  siteID: number,
  body: string,
  cronSpec?: string | null
): Promise<DeployScript> {
  return apiFetch<DeployScript>(`/v1/sites/${siteID}/deploy-script`, {
    method: 'PUT',
    body: JSON.stringify({ body, cron_spec: cronSpec ?? null }),
  })
}

export function listDeploys(siteID: number): Promise<Deploy[]> {
  return apiFetch<Deploy[]>(`/v1/sites/${siteID}/deploys`)
}

export function getDeploy(deployID: number): Promise<Deploy> {
  return apiFetch<Deploy>(`/v1/deploys/${deployID}`)
}

export function createDeploy(siteID: number): Promise<Deploy> {
  return apiFetch<Deploy>(`/v1/sites/${siteID}/deploys`, {
    method: 'POST',
    body: '{}',
  })
}
