import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Pause,
  Play,
  Plus,
  RotateCcw,
  ScrollText,
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
import { Switch } from '@/components/ui/switch'
import {
  createDaemon,
  daemonAction,
  deleteDaemon,
  getDaemonLogs,
  listDaemons,
  type CreateDaemonInput,
  type DaemonAction,
} from './daemons-api'

export function DaemonsPanel({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['daemons', siteID],
    queryFn: () => listDaemons(siteID),
  })

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Daemons</h2>
          <p className='text-muted-foreground text-sm'>
            Long-running commands managed by supervisor as the site's user.
          </p>
        </div>
        <NewDaemonDialog
          siteID={siteID}
          onCreated={() => qc.invalidateQueries({ queryKey: ['daemons', siteID] })}
        />
      </div>

      {(data?.length ?? 0) === 0 ? (
        <p className='text-muted-foreground text-sm'>No daemons yet.</p>
      ) : (
        <div className='space-y-2'>
          {data?.map((d) => (
            <DaemonRow key={d.id} siteID={siteID} daemonID={d.id} />
          ))}
        </div>
      )}
    </div>
  )
}

function DaemonRow({ siteID, daemonID }: { siteID: number; daemonID: number }) {
  const qc = useQueryClient()
  const [logsOpen, setLogsOpen] = useState(false)

  const { data: list } = useQuery({
    queryKey: ['daemons', siteID],
    queryFn: () => listDaemons(siteID),
  })
  const d = list?.find((x) => x.id === daemonID)
  if (!d) return null

  const act = useMutation({
    mutationFn: (action: DaemonAction) => daemonAction(siteID, daemonID, action),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['daemons', siteID] }),
  })
  const remove = useMutation({
    mutationFn: () => deleteDaemon(siteID, daemonID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['daemons', siteID] }),
  })

  return (
    <div className='border-border rounded-md border p-3'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0 flex-1 space-y-1'>
          <div className='flex items-center gap-2'>
            <p className='font-medium'>{d.name}</p>
            <Badge variant={statusVariant(d.status)}>{d.status}</Badge>
            {d.auto_restart && (
              <Badge variant='outline' className='text-xs'>
                auto-restart
              </Badge>
            )}
          </div>
          <p className='text-muted-foreground truncate font-mono text-xs'>
            {d.command}
          </p>
          {d.last_error && (
            <p className='text-destructive font-mono text-xs'>
              {d.last_error}
            </p>
          )}
        </div>
        <div className='flex items-center gap-1'>
          <IconButton
            title='Start'
            disabled={act.isPending}
            onClick={() => act.mutate('start')}
          >
            <Play className='h-4 w-4' />
          </IconButton>
          <IconButton
            title='Stop'
            disabled={act.isPending}
            onClick={() => act.mutate('stop')}
          >
            <Pause className='h-4 w-4' />
          </IconButton>
          <IconButton
            title='Restart'
            disabled={act.isPending}
            onClick={() => act.mutate('restart')}
          >
            <RotateCcw className='h-4 w-4' />
          </IconButton>
          <IconButton
            title='Logs'
            onClick={() => setLogsOpen(true)}
          >
            <ScrollText className='h-4 w-4' />
          </IconButton>
          <IconButton
            title='Delete'
            destructive
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(`Delete daemon "${d.name}"?`)) remove.mutate()
            }}
          >
            <Trash2 className='h-4 w-4' />
          </IconButton>
        </div>
      </div>

      <Dialog open={logsOpen} onOpenChange={setLogsOpen}>
        <DialogContent className='max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{d.name} — logs</DialogTitle>
          </DialogHeader>
          {logsOpen && <DaemonLogs siteID={siteID} daemonID={daemonID} />}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function DaemonLogs({ siteID, daemonID }: { siteID: number; daemonID: number }) {
  const { data, isLoading } = useQuery({
    queryKey: ['daemon-logs', siteID, daemonID],
    queryFn: () => getDaemonLogs(siteID, daemonID),
    refetchInterval: 3000,
  })
  return (
    <pre className='bg-muted/30 max-h-[60vh] overflow-auto rounded-md border p-3 font-mono text-xs whitespace-pre-wrap'>
      {isLoading ? 'Loading…' : data?.output || '(no output)'}
    </pre>
  )
}

function NewDaemonDialog({
  siteID,
  onCreated,
}: {
  siteID: number
  onCreated: () => void
}) {
  const [open, setOpen] = useState(false)
  const [slug, setSlug] = useState('')
  const [name, setName] = useState('')
  const [command, setCommand] = useState('')
  const [autoRestart, setAutoRestart] = useState(true)

  const create = useMutation({
    mutationFn: (input: CreateDaemonInput) => createDaemon(siteID, input),
    onSuccess: () => {
      onCreated()
      setOpen(false)
      setSlug('')
      setName('')
      setCommand('')
      setAutoRestart(true)
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className='mr-2 h-4 w-4' />
          Add daemon
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New daemon</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label htmlFor='slug'>Slug</Label>
            <Input
              id='slug'
              placeholder='queue-worker'
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
            />
            <p className='text-muted-foreground text-xs'>
              [a-z0-9-]+, used in the supervisor program name.
            </p>
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='name'>Name</Label>
            <Input
              id='name'
              placeholder='Laravel queue worker'
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='cmd'>Command</Label>
            <Input
              id='cmd'
              placeholder='php artisan queue:work --tries=3'
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              className='font-mono text-sm'
            />
          </div>
          <div className='flex items-center justify-between rounded-md border p-3'>
            <div>
              <Label htmlFor='auto'>Auto-restart</Label>
              <p className='text-muted-foreground text-xs'>
                Supervisor restarts the process if it exits non-zero.
              </p>
            </div>
            <Switch
              id='auto'
              checked={autoRestart}
              onCheckedChange={setAutoRestart}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            disabled={!slug || !command || create.isPending}
            onClick={() =>
              create.mutate({
                slug,
                name: name || slug,
                command,
                auto_restart: autoRestart,
              })
            }
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function IconButton({
  children,
  title,
  onClick,
  disabled,
  destructive,
}: {
  children: React.ReactNode
  title: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
}) {
  return (
    <Button
      title={title}
      size='icon'
      variant={destructive ? 'destructive' : 'outline'}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </Button>
  )
}

function statusVariant(
  status: string
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (status) {
    case 'running':
      return 'default'
    case 'stopped':
      return 'secondary'
    case 'error':
      return 'destructive'
    default:
      return 'outline'
  }
}
