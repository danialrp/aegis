import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Play } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { createDeploy, listDeploys, type DeployStatus } from './deploys-api'

const STATUS_VARIANT: Record<
  DeployStatus,
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
  queued: 'secondary',
  running: 'secondary',
  succeeded: 'default',
  failed: 'destructive',
}

export function DeploysPanel({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['deploys', siteID],
    queryFn: () => listDeploys(siteID),
    refetchInterval: (query) => {
      const rows = query.state.data ?? []
      return rows.some(
        (d) => d.status === 'queued' || d.status === 'running'
      )
        ? 2000
        : false
    },
  })

  const run = useMutation({
    mutationFn: () => createDeploy(siteID),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['deploys', siteID] })
    },
  })

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <h2 className='text-lg font-medium'>Deploys</h2>
        <Button
          onClick={() => run.mutate()}
          disabled={run.isPending}
        >
          <Play className='mr-2 h-4 w-4' />
          {run.isPending ? 'Queuing…' : 'Run now'}
        </Button>
      </div>

      {(data?.length ?? 0) === 0 ? (
        <p className='text-muted-foreground text-sm'>
          No deploys yet — click <b>Run now</b> to trigger the first.
        </p>
      ) : (
        <div className='rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>#</TableHead>
                <TableHead>Trigger</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Started</TableHead>
                <TableHead>Exit</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data?.map((d) => (
                <TableRow key={d.id}>
                  <TableCell>
                    <Link
                      to='/deploys/$deployId'
                      params={{ deployId: String(d.id) }}
                      className='font-mono text-sm hover:underline'
                    >
                      #{d.id}
                    </Link>
                  </TableCell>
                  <TableCell className='text-sm'>{d.trigger}</TableCell>
                  <TableCell>
                    <Badge variant={STATUS_VARIANT[d.status]}>
                      {d.status}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    {d.started_at
                      ? new Date(d.started_at).toLocaleString()
                      : '—'}
                  </TableCell>
                  <TableCell className='font-mono text-sm'>
                    {d.exit_code ?? '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
