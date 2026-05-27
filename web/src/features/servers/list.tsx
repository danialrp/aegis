import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { listServers, type Server } from './api'
import { StatusBadge } from './status-badge'

export function ServersList() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['servers'],
    queryFn: listServers,
    // Poll while any server is mid-provisioning so the badge updates
    // without the user reloading.
    refetchInterval: (query) => {
      const rows = (query.state.data ?? []) as Server[]
      const inFlight = rows.some(
        (s) => s.status === 'pending' || s.status === 'provisioning'
      )
      return inFlight ? 2000 : false
    },
  })

  return (
    <>
      <Header />
      <Main>
        <div className='mb-6 flex items-center justify-between'>
          <div>
            <h1 className='text-2xl font-semibold'>Servers</h1>
            <p className='text-muted-foreground text-sm'>
              Linux hosts under Aegis management.
            </p>
          </div>
          <Button asChild>
            <Link to='/servers/new'>
              <Plus className='mr-2 h-4 w-4' />
              Add server
            </Link>
          </Button>
        </div>

        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && (
          <p className='text-destructive'>Failed to load servers.</p>
        )}

        {!isLoading && !error && (data?.length ?? 0) === 0 && (
          <div className='rounded-md border border-dashed p-10 text-center'>
            <h2 className='text-lg font-medium'>No servers yet</h2>
            <p className='text-muted-foreground mb-4 text-sm'>
              Add a Linux host to bring it under management.
            </p>
            <Button asChild>
              <Link to='/servers/new'>Add your first server</Link>
            </Button>
          </div>
        )}

        {!isLoading && (data?.length ?? 0) > 0 && (
          <div className='rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Public IP</TableHead>
                  <TableHead>SSH user</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last seen</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>
                      <Link
                        to='/servers/$id'
                        params={{ id: String(s.id) }}
                        className='font-medium hover:underline'
                      >
                        {s.name}
                      </Link>
                    </TableCell>
                    <TableCell className='font-mono text-sm'>
                      {s.public_ip}
                    </TableCell>
                    <TableCell>{s.ssh_user}</TableCell>
                    <TableCell>
                      <StatusBadge status={s.status} />
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {s.agent_last_seen
                        ? new Date(s.agent_last_seen).toLocaleString()
                        : '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </Main>
    </>
  )
}
