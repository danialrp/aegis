// Databases API client. Mirrors internal/api/v1/databases.go.
import { apiFetch } from '@/lib/api'

export type DBEngine = 'mysql' | 'postgres'
export type DBStatus = 'pending' | 'creating' | 'ready' | 'error'

export interface SiteDatabase {
  id: number
  site_id: number
  engine: DBEngine
  name: string
  username: string
  password: string
  status: DBStatus
  last_error?: string | null
  last_backup_at?: string | null
  created_at: string
}

export interface CreateDatabaseInput {
  engine: DBEngine
  name: string
  username: string
  password?: string // optional — server generates if empty
}

export interface BackupRow {
  basename: string
  size: number
  mtime: number
}

export function listDatabases(siteID: number): Promise<SiteDatabase[]> {
  return apiFetch<SiteDatabase[]>(`/v1/sites/${siteID}/databases`)
}

export function createDatabase(
  siteID: number,
  input: CreateDatabaseInput
): Promise<SiteDatabase> {
  return apiFetch<SiteDatabase>(`/v1/sites/${siteID}/databases`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function deleteDatabase(siteID: number, dbID: number): Promise<void> {
  return apiFetch<void>(`/v1/sites/${siteID}/databases/${dbID}`, {
    method: 'DELETE',
  })
}

export function backupDatabase(
  siteID: number,
  dbID: number
): Promise<{ path: string }> {
  return apiFetch<{ path: string }>(
    `/v1/sites/${siteID}/databases/${dbID}/backup`,
    { method: 'POST', body: '{}' }
  )
}

export function listBackups(
  siteID: number,
  dbID: number
): Promise<BackupRow[]> {
  return apiFetch<BackupRow[]>(
    `/v1/sites/${siteID}/databases/${dbID}/backups`
  )
}

export function restoreBackup(
  siteID: number,
  dbID: number,
  basename: string
): Promise<void> {
  return apiFetch<void>(`/v1/sites/${siteID}/databases/${dbID}/restore`, {
    method: 'POST',
    body: JSON.stringify({ basename }),
  })
}
