import { useQuery } from '@tanstack/react-query'
import { Cpu, HardDrive, MemoryStick, Server } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { apiFetch, isApiError } from '@/lib/api'

interface Metrics {
  collected_at: number
  uptime_sec: number
  load_avg: [number, number, number]
  cpu_count: number
  memory: { total: number; used: number; free: number }
  swap: { total: number; used: number; free: number }
  disks: { mount: string; fs: string; total: number; used: number }[]
  kernel?: string
}

export function MetricsPanel({ serverID }: { serverID: number }) {
  const { data, error } = useQuery({
    queryKey: ['server-metrics', serverID],
    queryFn: () =>
      apiFetch<Metrics>(`/v1/servers/${serverID}/metrics`),
    refetchInterval: 5000,
    retry: false,
  })

  if (error) {
    const code = isApiError(error) ? error.code : 'unknown'
    return (
      <div className='border-muted bg-muted/30 rounded-md border p-4 text-sm'>
        <p className='font-medium'>Metrics unavailable</p>
        <p className='text-muted-foreground mt-1 text-xs'>
          {code === 'agent_offline'
            ? 'Agent is not currently connected. Metrics resume automatically once it reconnects.'
            : code}
        </p>
      </div>
    )
  }

  if (!data) {
    return (
      <p className='text-muted-foreground text-sm'>Loading metrics…</p>
    )
  }

  const memPct = data.memory.total
    ? Math.round((data.memory.used / data.memory.total) * 100)
    : 0
  const swapPct = data.swap.total
    ? Math.round((data.swap.used / data.swap.total) * 100)
    : 0

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Server metrics</h2>
          <p className='text-muted-foreground text-sm'>
            Polled every 5s · uptime {formatUptime(data.uptime_sec)}
            {data.kernel ? ` · kernel ${data.kernel}` : ''}
          </p>
        </div>
        <Badge variant='outline' className='text-xs'>
          {data.cpu_count} CPU{data.cpu_count === 1 ? '' : 's'}
        </Badge>
      </div>

      <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
        <Card
          icon={<Cpu className='h-5 w-5' />}
          label='Load average'
          value={`${data.load_avg[0].toFixed(2)} · ${data.load_avg[1].toFixed(2)} · ${data.load_avg[2].toFixed(2)}`}
          hint='1m · 5m · 15m'
        />
        <Card
          icon={<MemoryStick className='h-5 w-5' />}
          label='Memory'
          value={`${formatBytes(data.memory.used)} / ${formatBytes(data.memory.total)}`}
          hint={`${memPct}% used`}
        />
        <Card
          icon={<Server className='h-5 w-5' />}
          label='Swap'
          value={
            data.swap.total
              ? `${formatBytes(data.swap.used)} / ${formatBytes(data.swap.total)}`
              : 'none'
          }
          hint={data.swap.total ? `${swapPct}% used` : 'no swap configured'}
        />
      </div>

      {data.disks.length > 0 && (
        <div className='space-y-2'>
          <p className='flex items-center gap-2 text-sm font-medium'>
            <HardDrive className='h-4 w-4' />
            Filesystems
          </p>
          <div className='space-y-1'>
            {data.disks.map((d) => {
              const pct = d.total ? Math.round((d.used / d.total) * 100) : 0
              return (
                <div
                  key={d.mount}
                  className='border-border flex items-center justify-between rounded-md border px-3 py-2 text-sm'
                >
                  <div className='font-mono text-xs'>
                    {d.mount}
                    <span className='text-muted-foreground ml-2'>
                      {d.fs}
                    </span>
                  </div>
                  <div className='font-mono text-xs'>
                    {formatBytes(d.used)} / {formatBytes(d.total)}{' '}
                    <span className='text-muted-foreground'>({pct}%)</span>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}

function Card({
  icon,
  label,
  value,
  hint,
}: {
  icon: React.ReactNode
  label: string
  value: string
  hint: string
}) {
  return (
    <div className='border-border rounded-md border p-3'>
      <div className='text-muted-foreground flex items-center gap-2 text-xs'>
        {icon}
        {label}
      </div>
      <div className='mt-1 font-mono text-lg'>{value}</div>
      <div className='text-muted-foreground text-xs'>{hint}</div>
    </div>
  )
}

function formatBytes(n: number): string {
  if (!n) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}

function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}
