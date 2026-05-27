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
import { listSites, type Site } from './api'
import { SiteStatusBadge } from './status-badge'

export function SitesList() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['sites'],
    queryFn: listSites,
    refetchInterval: (query) => {
      const rows = (query.state.data ?? []) as Site[]
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
            <h1 className='text-2xl font-semibold'>Sites</h1>
            <p className='text-muted-foreground text-sm'>
              Per-site Linux users, isolated working directories, deploy
              scripts.
            </p>
          </div>
          <Button asChild>
            <Link to='/sites/new'>
              <Plus className='mr-2 h-4 w-4' />
              Add site
            </Link>
          </Button>
        </div>

        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && (
          <p className='text-destructive'>Failed to load sites.</p>
        )}

        {!isLoading && !error && (data?.length ?? 0) === 0 && (
          <div className='rounded-md border border-dashed p-10 text-center'>
            <h2 className='text-lg font-medium'>No sites yet</h2>
            <p className='text-muted-foreground mb-4 text-sm'>
              Add a site to start serving content from a managed server.
            </p>
            <Button asChild>
              <Link to='/sites/new'>Add your first site</Link>
            </Button>
          </div>
        )}

        {!isLoading && (data?.length ?? 0) > 0 && (
          <div className='rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Domain</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Server</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>
                      <Link
                        to='/sites/$id'
                        params={{ id: String(s.id) }}
                        className='font-medium hover:underline'
                      >
                        {s.name}
                      </Link>
                    </TableCell>
                    <TableCell className='font-mono text-sm'>
                      {s.domain}
                    </TableCell>
                    <TableCell>
                      <span className='font-mono text-xs uppercase'>
                        {s.site_type}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Link
                        to='/servers/$id'
                        params={{ id: String(s.server_id) }}
                        className='text-muted-foreground hover:underline'
                      >
                        #{s.server_id}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <SiteStatusBadge status={s.status} />
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
