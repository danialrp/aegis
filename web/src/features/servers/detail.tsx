import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { deleteServer, getServer } from './api'
import { MetricsPanel } from './metrics-panel'
import { StatusBadge } from './status-badge'

export function ServerDetail({ id }: { id: number }) {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data, isLoading, error } = useQuery({
    queryKey: ['servers', id],
    queryFn: () => getServer(id),
    refetchInterval: (query) => {
      const s = query.state.data
      if (!s) return false
      return s.status === 'pending' || s.status === 'provisioning'
        ? 2000
        : false
    },
  })

  const remove = useMutation({
    mutationFn: () => deleteServer(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['servers'] })
      await navigate({ to: '/servers' })
    },
  })

  return (
    <>
      <Header />
      <Main>
        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && <p className='text-destructive'>Failed to load server.</p>}

        {data && (
          <div className='mx-auto max-w-3xl space-y-6'>
            <div className='flex items-start justify-between'>
              <div>
                <h1 className='text-2xl font-semibold'>{data.name}</h1>
                <p className='text-muted-foreground font-mono text-sm'>
                  {data.public_ip}
                </p>
              </div>
              <StatusBadge status={data.status} />
            </div>

            {data.provision_error && (
              <div className='border-destructive bg-destructive/10 rounded-md border p-4'>
                <p className='text-destructive text-sm font-medium'>
                  Provisioning error
                </p>
                <p className='text-destructive mt-1 font-mono text-xs'>
                  {data.provision_error}
                </p>
              </div>
            )}

            <dl className='divide-border grid divide-y rounded-md border'>
              <Field label='SSH user' value={data.ssh_user} />
              <Field
                label='Agent fingerprint'
                value={data.agent_fingerprint ?? '—'}
                mono
              />
              <Field
                label='Last seen'
                value={
                  data.agent_last_seen
                    ? new Date(data.agent_last_seen).toLocaleString()
                    : '—'
                }
              />
              <Field
                label='Created'
                value={new Date(data.created_at).toLocaleString()}
              />
            </dl>

            {data.status === 'ready' && <MetricsPanel serverID={data.id} />}

            <div className='flex justify-end'>
              <Button
                variant='destructive'
                onClick={() => {
                  if (
                    window.confirm(
                      `Delete ${data.name}? This removes the row only — the agent service on the target host must be uninstalled manually.`
                    )
                  ) {
                    remove.mutate()
                  }
                }}
                disabled={remove.isPending}
              >
                {remove.isPending ? 'Deleting…' : 'Delete server'}
              </Button>
            </div>
          </div>
        )}
      </Main>
    </>
  )
}

function Field({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className='grid grid-cols-3 gap-4 px-4 py-3'>
      <dt className='text-muted-foreground text-sm'>{label}</dt>
      <dd
        className={
          'col-span-2 text-sm ' + (mono ? 'font-mono break-all' : '')
        }
      >
        {value}
      </dd>
    </div>
  )
}
