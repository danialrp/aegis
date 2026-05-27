import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  Copy,
  Database,
  Eye,
  EyeOff,
  History,
  Plus,
  Trash2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  backupDatabase,
  createDatabase,
  deleteDatabase,
  listBackups,
  listDatabases,
  restoreBackup,
  type CreateDatabaseInput,
  type DBEngine,
  type SiteDatabase,
} from './databases-api'

export function DatabasesPanel({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['databases', siteID],
    queryFn: () => listDatabases(siteID),
  })
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Databases</h2>
          <p className='text-muted-foreground text-sm'>
            Per-site MySQL (MariaDB) + Postgres databases. Backups land
            under <span className='font-mono'>/srv/sites/{siteID}/backups/</span>.
          </p>
        </div>
        <NewDBDialog
          siteID={siteID}
          onCreated={() => qc.invalidateQueries({ queryKey: ['databases', siteID] })}
        />
      </div>

      {(data?.length ?? 0) === 0 ? (
        <p className='text-muted-foreground text-sm'>No databases yet.</p>
      ) : (
        <div className='space-y-2'>
          {data?.map((d) => (
            <DBRow key={d.id} siteID={siteID} db={d} />
          ))}
        </div>
      )}
    </div>
  )
}

function DBRow({ siteID, db }: { siteID: number; db: SiteDatabase }) {
  const qc = useQueryClient()
  const [showPw, setShowPw] = useState(false)
  const [backupsOpen, setBackupsOpen] = useState(false)

  const backup = useMutation({
    mutationFn: () => backupDatabase(siteID, db.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['databases', siteID] }),
  })
  const remove = useMutation({
    mutationFn: () => deleteDatabase(siteID, db.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['databases', siteID] }),
  })

  return (
    <div className='border-border rounded-md border p-3'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0 flex-1 space-y-1'>
          <div className='flex items-center gap-2'>
            <Database className='h-4 w-4' />
            <p className='font-medium'>{db.name}</p>
            <Badge variant='outline' className='text-xs uppercase'>
              {db.engine}
            </Badge>
            <Badge variant={statusVariant(db.status)}>{db.status}</Badge>
          </div>
          <p className='text-muted-foreground font-mono text-xs'>
            user: {db.username}
          </p>
          <div className='flex items-center gap-2 text-xs'>
            <span className='text-muted-foreground'>password:</span>
            <span className='font-mono'>
              {showPw ? db.password : '•'.repeat(Math.min(db.password.length, 24))}
            </span>
            <Button
              size='icon'
              variant='ghost'
              className='h-6 w-6'
              onClick={() => setShowPw((v) => !v)}
            >
              {showPw ? <EyeOff className='h-3 w-3' /> : <Eye className='h-3 w-3' />}
            </Button>
            <Button
              size='icon'
              variant='ghost'
              className='h-6 w-6'
              onClick={() => navigator.clipboard.writeText(db.password)}
              title='Copy password'
            >
              <Copy className='h-3 w-3' />
            </Button>
          </div>
          {db.last_error && (
            <p className='text-destructive font-mono text-xs'>{db.last_error}</p>
          )}
          {db.last_backup_at && (
            <p className='text-muted-foreground text-xs'>
              Last backup:{' '}
              {new Date(db.last_backup_at).toLocaleString()}
            </p>
          )}
        </div>
        <div className='flex items-center gap-1'>
          <Button
            size='sm'
            variant='outline'
            onClick={() => backup.mutate()}
            disabled={backup.isPending}
          >
            <Archive className='mr-2 h-4 w-4' />
            {backup.isPending ? 'Backing up…' : 'Backup now'}
          </Button>
          <Button
            size='icon'
            variant='outline'
            title='Backups & restore'
            onClick={() => setBackupsOpen(true)}
          >
            <History className='h-4 w-4' />
          </Button>
          <Button
            size='icon'
            variant='destructive'
            disabled={remove.isPending}
            onClick={() => {
              if (
                window.confirm(
                  `Drop database "${db.name}" and user "${db.username}"? This deletes data permanently.`
                )
              )
                remove.mutate()
            }}
            title='Delete database'
          >
            <Trash2 className='h-4 w-4' />
          </Button>
        </div>
      </div>

      <Dialog open={backupsOpen} onOpenChange={setBackupsOpen}>
        <DialogContent className='max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {db.engine}/{db.name} — backups
            </DialogTitle>
          </DialogHeader>
          {backupsOpen && <BackupsList siteID={siteID} db={db} />}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function BackupsList({
  siteID,
  db,
}: {
  siteID: number
  db: SiteDatabase
}) {
  const { data, isLoading } = useQuery({
    queryKey: ['backups', siteID, db.id],
    queryFn: () => listBackups(siteID, db.id),
  })
  const qc = useQueryClient()
  const restore = useMutation({
    mutationFn: (basename: string) => restoreBackup(siteID, db.id, basename),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['backups', siteID, db.id] }),
  })

  if (isLoading) return <p className='text-muted-foreground'>Loading…</p>
  if ((data?.length ?? 0) === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        No backups yet. Click <b>Backup now</b> on the row to create the first.
      </p>
    )
  }
  return (
    <div className='space-y-2'>
      {data?.map((b) => (
        <div
          key={b.basename}
          className='border-border flex items-center justify-between rounded-md border p-2'
        >
          <div className='min-w-0 flex-1'>
            <p className='truncate font-mono text-xs'>{b.basename}</p>
            <p className='text-muted-foreground text-xs'>
              {formatBytes(b.size)} ·{' '}
              {new Date(b.mtime * 1000).toLocaleString()}
            </p>
          </div>
          <Button
            size='sm'
            variant='outline'
            disabled={restore.isPending}
            onClick={() => {
              if (
                window.confirm(
                  `Restore ${b.basename} into ${db.name}? This OVERWRITES current data.`
                )
              )
                restore.mutate(b.basename)
            }}
          >
            Restore
          </Button>
        </div>
      ))}
    </div>
  )
}

function NewDBDialog({
  siteID,
  onCreated,
}: {
  siteID: number
  onCreated: () => void
}) {
  const [open, setOpen] = useState(false)
  const [engine, setEngine] = useState<DBEngine>('mysql')
  const [name, setName] = useState('')
  const [username, setUsername] = useState('')

  const create = useMutation({
    mutationFn: (input: CreateDatabaseInput) => createDatabase(siteID, input),
    onSuccess: () => {
      onCreated()
      setOpen(false)
      setName('')
      setUsername('')
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className='mr-2 h-4 w-4' />
          New database
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New database</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label htmlFor='engine'>Engine</Label>
            <Select value={engine} onValueChange={(v) => setEngine(v as DBEngine)}>
              <SelectTrigger id='engine'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='mysql'>MySQL (MariaDB)</SelectItem>
                <SelectItem value='postgres'>PostgreSQL</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='name'>Database name</Label>
            <Input
              id='name'
              placeholder='myapp_prod'
              value={name}
              onChange={(e) => setName(e.target.value)}
              className='font-mono'
            />
            <p className='text-muted-foreground text-xs'>
              Letters / digits / underscore only.
            </p>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='user'>User name</Label>
            <Input
              id='user'
              placeholder='myapp'
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className='font-mono'
            />
            <p className='text-muted-foreground text-xs'>
              Password is generated server-side and revealed once after create.
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            disabled={!name || !username || create.isPending}
            onClick={() => create.mutate({ engine, name, username })}
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function statusVariant(
  status: string
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'ready':
      return 'default'
    case 'pending':
    case 'creating':
      return 'secondary'
    case 'error':
      return 'destructive'
    default:
      return 'outline'
  }
}

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KiB`
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MiB`
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}
