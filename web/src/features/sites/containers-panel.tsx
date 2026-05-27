import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ChevronsDown,
  Hammer,
  PlayCircle,
  RotateCcw,
  ScrollText,
  Square,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  composeAction,
  getContainerLogs,
  listContainers,
  type ComposeAction,
} from './docker-api'

export function ContainersPanel({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const [logsFor, setLogsFor] = useState<string | null>(null)

  const { data } = useQuery({
    queryKey: ['containers', siteID],
    queryFn: () => listContainers(siteID),
    refetchInterval: 5000,
  })

  const act = useMutation({
    mutationFn: (action: ComposeAction) => composeAction(siteID, action),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['containers', siteID] }),
  })

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Containers</h2>
          <p className='text-muted-foreground text-sm'>
            Compose project <span className='font-mono'>aegis-site-{siteID}</span>.
            Polls every 5s.
          </p>
        </div>
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            disabled={act.isPending}
            onClick={() => act.mutate('pull')}
          >
            <ChevronsDown className='mr-2 h-4 w-4' />
            Pull
          </Button>
          <Button
            variant='outline'
            disabled={act.isPending}
            onClick={() => act.mutate('build')}
          >
            <Hammer className='mr-2 h-4 w-4' />
            Build
          </Button>
          <Button
            variant='outline'
            disabled={act.isPending}
            onClick={() => act.mutate('restart')}
          >
            <RotateCcw className='mr-2 h-4 w-4' />
            Restart
          </Button>
          <Button
            disabled={act.isPending}
            onClick={() => act.mutate('up')}
          >
            <PlayCircle className='mr-2 h-4 w-4' />
            Up
          </Button>
          <Button
            variant='destructive'
            disabled={act.isPending}
            onClick={() => {
              if (window.confirm('Stop and remove all containers?'))
                act.mutate('down')
            }}
          >
            <Square className='mr-2 h-4 w-4' />
            Down
          </Button>
        </div>
      </div>

      {(data?.length ?? 0) === 0 ? (
        <p className='text-muted-foreground text-sm'>
          No containers running. Click <b>Up</b> after saving a compose file.
        </p>
      ) : (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Service</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Image</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className='w-12'></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.map((c) => (
                <TableRow key={c.name}>
                  <TableCell className='font-mono text-sm'>
                    {c.service}
                  </TableCell>
                  <TableCell className='text-muted-foreground font-mono text-xs'>
                    {c.name}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {c.image}
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateVariant(c.state)}>{c.state}</Badge>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {c.status}
                  </TableCell>
                  <TableCell>
                    <Button
                      size='icon'
                      variant='outline'
                      title='Logs'
                      onClick={() => setLogsFor(c.service)}
                    >
                      <ScrollText className='h-4 w-4' />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={logsFor !== null} onOpenChange={() => setLogsFor(null)}>
        <DialogContent className='max-w-3xl'>
          <DialogHeader>
            <DialogTitle>
              {logsFor} — logs
            </DialogTitle>
          </DialogHeader>
          {logsFor && <ContainerLogs siteID={siteID} service={logsFor} />}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function ContainerLogs({
  siteID,
  service,
}: {
  siteID: number
  service: string
}) {
  const { data, isLoading } = useQuery({
    queryKey: ['container-logs', siteID, service],
    queryFn: () => getContainerLogs(siteID, service),
    refetchInterval: 3000,
  })
  return (
    <pre className='bg-muted/30 max-h-[60vh] overflow-auto rounded-md border p-3 font-mono text-xs whitespace-pre-wrap'>
      {isLoading ? 'Loading…' : data?.output || '(no output)'}
    </pre>
  )
}

function stateVariant(
  state: string
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (state) {
    case 'running':
      return 'default'
    case 'exited':
    case 'dead':
      return 'destructive'
    case 'paused':
      return 'secondary'
    default:
      return 'outline'
  }
}
