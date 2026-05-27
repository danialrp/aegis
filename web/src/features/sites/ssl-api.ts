// SSL / certificate API client. Mirrors internal/api/v1/ssl.go.
import { apiFetch } from '@/lib/api'

export type CertStatus =
  | 'pending'
  | 'issuing'
  | 'active'
  | 'expired'
  | 'error'

export interface Cert {
  id: number
  site_id: number
  domain: string
  status: CertStatus
  issued_at?: string | null
  expires_at?: string | null
  last_error?: string | null
  created_at: string
  updated_at: string
}

export function listCerts(siteID: number): Promise<Cert[]> {
  return apiFetch<Cert[]>(`/v1/sites/${siteID}/certs`)
}

export function createCert(siteID: number): Promise<Cert> {
  return apiFetch<Cert>(`/v1/sites/${siteID}/certs`, {
    method: 'POST',
    body: '{}',
  })
}

export function deleteCert(siteID: number, certID: number): Promise<void> {
  return apiFetch<void>(`/v1/sites/${siteID}/certs/${certID}`, {
    method: 'DELETE',
  })
}
