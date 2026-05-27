// Terminal API client. Mirrors internal/api/v1/terminal.go.
import { apiFetch } from '@/lib/api'

export interface TerminalTicket {
  ticket: string
  expires_sec: number
}

export function issueTerminalTicket(siteID: number): Promise<TerminalTicket> {
  return apiFetch<TerminalTicket>(
    `/v1/sites/${siteID}/terminal/ticket`,
    { method: 'POST', body: '{}' }
  )
}
