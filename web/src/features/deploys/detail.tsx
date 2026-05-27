import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { getDeploy, type DeployStatus } from '@/features/sites/deploys-api'

const STATUS_VARIANT: Record<
  DeployStatus,
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
  queued: 'secondary',
  running: 'secondary',
  succeeded: 'default',
  failed: 'destructive',
}

export function DeployDetail({ deployID }: { deployID: number }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['deploys', 'detail', deployID],
    queryFn: () => getDeploy(deployID),
    refetchInterval: (query) => {
      const d = query.state.data
      if (!d) return 1000
      return d.status === 'queued' || d.status === 'running' ? 1000 : false
    },
  })

  return (
    <>
      <Header />
      <Main>
        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && (
          <p className='text-destructive'>Failed to load deploy.</p>
        )}
        {data && (
          <div className='space-y-4'>
            <div className='flex items-start justify-between'>
              <div>
                <h1 className='text-xl font-semibold'>
                  Deploy <span className='font-mono'>#{data.id}</span>
                </h1>
                <p className='text-muted-foreground text-sm'>
                  <Link
                    to='/sites/$id'
                    params={{ id: String(data.site_id) }}
                    className='hover:underline'
                  >
                    site #{data.site_id}
                  </Link>{' '}
                  · trigger: {data.trigger} · exit:{' '}
                  <span className='font-mono'>
                    {data.exit_code ?? '—'}
                  </span>
                </p>
              </div>
              <Badge variant={STATUS_VARIANT[data.status]}>
                {data.status}
              </Badge>
            </div>

            <pre className='border-border bg-muted/30 max-h-[600px] overflow-auto rounded-md border p-4 font-mono text-xs whitespace-pre-wrap'>
              {data.output_log || '(no output yet)'}
            </pre>
          </div>
        )}
      </Main>
    </>
  )
}
