import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { deleteSite, getSite } from './api'
import { AdapterHint } from './adapter-hint'
import { ComposeEditor } from './compose-editor'
import { ContainersPanel } from './containers-panel'
import { DaemonsPanel } from './daemons-panel'
import { DatabasesPanel } from './databases-panel'
import { DeployScriptEditor } from './deploy-script-editor'
import { DeploysPanel } from './deploys-panel'
import { SSLPanel } from './ssl-panel'
import { SiteStatusBadge } from './status-badge'

export function SiteDetail({ id }: { id: number }) {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data, isLoading, error } = useQuery({
    queryKey: ['sites', id],
    queryFn: () => getSite(id),
    refetchInterval: (query) => {
      const s = query.state.data
      if (!s) return false
      return s.status === 'pending' || s.status === 'provisioning'
        ? 2000
        : false
    },
  })

  const remove = useMutation({
    mutationFn: () => deleteSite(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['sites'] })
      await navigate({ to: '/sites' })
    },
  })

  return (
    <>
      <Header />
      <Main>
        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && <p className='text-destructive'>Failed to load site.</p>}

        {data && (
          <div className='mx-auto max-w-3xl space-y-6'>
            <div className='flex items-start justify-between'>
              <div>
                <h1 className='text-2xl font-semibold'>{data.name}</h1>
                <p className='text-muted-foreground font-mono text-sm'>
                  {data.domain}
                </p>
              </div>
              <SiteStatusBadge status={data.status} />
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
              <Field label='Type' value={data.site_type.toUpperCase()} />
              <Field
                label='Server'
                value={
                  <Link
                    to='/servers/$id'
                    params={{ id: String(data.server_id) }}
                    className='hover:underline'
                  >
                    #{data.server_id}
                  </Link>
                }
              />
              <Field
                label='Working directory'
                value={data.working_dir}
                mono
              />
              <Field
                label='Created'
                value={new Date(data.created_at).toLocaleString()}
              />
            </dl>

            <AdapterHint
              siteType={data.site_type}
              proxyPort={data.proxy_port}
            />

            <SSLPanel siteID={data.id} />

            {data.site_type === 'docker' && (
              <>
                <ComposeEditor siteID={data.id} />
                <ContainersPanel siteID={data.id} />
              </>
            )}

            <DeployScriptEditor siteID={data.id} />

            <DeploysPanel siteID={data.id} />

            <DaemonsPanel siteID={data.id} />

            <DatabasesPanel siteID={data.id} />

            <div className='flex justify-end'>
              <Button
                variant='destructive'
                onClick={() => {
                  if (
                    window.confirm(
                      `Delete site "${data.name}"? The DB row will be removed; in 1.1 host-side teardown is manual.`
                    )
                  ) {
                    remove.mutate()
                  }
                }}
                disabled={remove.isPending}
              >
                {remove.isPending ? 'Deleting…' : 'Delete site'}
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
  value: React.ReactNode
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
