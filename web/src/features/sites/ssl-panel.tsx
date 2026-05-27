import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Lock, ShieldAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { createCert, listCerts, type CertStatus } from './ssl-api'

const STATUS_VARIANT: Record<
  CertStatus,
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
  pending: 'secondary',
  issuing: 'secondary',
  active: 'default',
  expired: 'destructive',
  error: 'destructive',
}

export function SSLPanel({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data } = useQuery({
    queryKey: ['certs', siteID],
    queryFn: () => listCerts(siteID),
    refetchInterval: (query) => {
      const rows = query.state.data ?? []
      return rows.some(
        (c) => c.status === 'pending' || c.status === 'issuing'
      )
        ? 2000
        : false
    },
  })

  const issue = useMutation({
    mutationFn: () => createCert(siteID),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['certs', siteID] })
    },
  })

  const active = data?.find((c) => c.status === 'active')

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>HTTPS</h2>
          <p className='text-muted-foreground text-sm'>
            Let's Encrypt cert via certbot's HTTP-01 challenge.
          </p>
        </div>
        {!active && (
          <Button
            onClick={() => issue.mutate()}
            disabled={issue.isPending}
          >
            <Lock className='mr-2 h-4 w-4' />
            {issue.isPending ? 'Requesting…' : 'Enable HTTPS'}
          </Button>
        )}
      </div>

      {(data?.length ?? 0) === 0 && !issue.isPending && (
        <p className='text-muted-foreground text-sm'>
          No certs yet. Click <b>Enable HTTPS</b> once the site is
          reachable on port 80.
        </p>
      )}

      {data?.map((c) => (
        <div
          key={c.id}
          className='border-border flex items-start justify-between rounded-md border p-3'
        >
          <div className='space-y-1'>
            <p className='font-mono text-sm'>{c.domain}</p>
            <p className='text-muted-foreground text-xs'>
              {c.issued_at &&
                `Issued ${new Date(c.issued_at).toLocaleString()} · `}
              {c.expires_at &&
                `expires ${new Date(c.expires_at).toLocaleString()}`}
            </p>
            {c.last_error && (
              <p className='text-destructive flex items-start gap-1 text-xs'>
                <ShieldAlert className='mt-0.5 h-3 w-3 shrink-0' />
                <span className='font-mono'>{c.last_error}</span>
              </p>
            )}
          </div>
          <Badge variant={STATUS_VARIANT[c.status]}>{c.status}</Badge>
        </div>
      ))}
    </div>
  )
}
