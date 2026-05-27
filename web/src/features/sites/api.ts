// Sites-domain API client. Mirrors the response shapes in
// internal/api/v1/sites.go.
import { apiFetch } from '@/lib/api'

export type SiteType =
  | 'static'
  | 'php'
  | 'laravel'
  | 'wordpress'
  | 'nextjs'
  | 'docker'
export type SiteStatus = 'pending' | 'provisioning' | 'ready' | 'error'

export interface Site {
  id: number
  server_id: number
  name: string
  domain: string
  site_type: SiteType
  status: SiteStatus
  working_dir: string
  proxy_port?: number | null
  provision_error?: string | null
  created_at: string
  updated_at: string
}

export interface CreateSiteInput {
  server_id: number
  name: string
  domain: string
  site_type: SiteType
  proxy_port?: number
}

export function listSites(): Promise<Site[]> {
  return apiFetch<Site[]>('/v1/sites')
}

export function getSite(id: number): Promise<Site> {
  return apiFetch<Site>(`/v1/sites/${id}`)
}

export function createSite(input: CreateSiteInput): Promise<Site> {
  return apiFetch<Site>('/v1/sites', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function deleteSite(id: number): Promise<void> {
  return apiFetch<void>(`/v1/sites/${id}`, { method: 'DELETE' })
}
