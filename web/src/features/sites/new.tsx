import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { isApiError } from '@/lib/api'
import { listServers } from '@/features/servers/api'
import { createSite, type CreateSiteInput, type SiteType } from './api'

export function NewSiteForm() {
  const navigate = useNavigate()
  const qc = useQueryClient()

  const servers = useQuery({
    queryKey: ['servers'],
    queryFn: listServers,
  })

  const [serverID, setServerID] = useState<string>('')
  const [name, setName] = useState('')
  const [domain, setDomain] = useState('')
  const [siteType, setSiteType] = useState<SiteType>('static')

  const mutation = useMutation({
    mutationFn: (input: CreateSiteInput) => createSite(input),
    onSuccess: async (created) => {
      await qc.invalidateQueries({ queryKey: ['sites'] })
      await navigate({
        to: '/sites/$id',
        params: { id: String(created.id) },
      })
    },
  })

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const sid = Number(serverID)
    if (!sid) return
    mutation.mutate({
      server_id: sid,
      name: name.trim(),
      domain: domain.trim(),
      site_type: siteType,
    })
  }

  const errorMessage =
    mutation.error && isApiError(mutation.error)
      ? mutation.error.code
      : mutation.error
        ? 'Failed to add site'
        : null

  const readyServers = (servers.data ?? []).filter(
    (s) => s.status === 'ready'
  )

  return (
    <>
      <Header />
      <Main>
        <div className='mx-auto max-w-xl'>
          <h1 className='mb-1 text-2xl font-semibold'>Add site</h1>
          <p className='text-muted-foreground mb-6 text-sm'>
            A site is hosted on one managed server, has its own Linux user
            and isolated working directory, and serves a single domain.
          </p>

          {servers.data && readyServers.length === 0 && (
            <div className='border-muted bg-muted/30 mb-6 rounded-md border p-4 text-sm'>
              No ready servers yet. Add a server first — sites need a host
              to live on.
            </div>
          )}

          <form className='space-y-4' onSubmit={onSubmit}>
            <div className='grid gap-2'>
              <Label htmlFor='server'>Server</Label>
              <Select value={serverID} onValueChange={setServerID}>
                <SelectTrigger id='server'>
                  <SelectValue placeholder='Pick a server' />
                </SelectTrigger>
                <SelectContent>
                  {readyServers.map((s) => (
                    <SelectItem key={s.id} value={String(s.id)}>
                      {s.name} ({s.public_ip})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='name'>Name</Label>
              <Input
                id='name'
                placeholder='marketing-site'
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='domain'>Domain</Label>
              <Input
                id='domain'
                placeholder='example.com'
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                required
              />
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='type'>Type</Label>
              <Select
                value={siteType}
                onValueChange={(v) => setSiteType(v as SiteType)}
              >
                <SelectTrigger id='type'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='static'>Static (HTML / SPA)</SelectItem>
                  <SelectItem value='php' disabled>
                    PHP — Phase 4
                  </SelectItem>
                  <SelectItem value='docker' disabled>
                    Docker — Phase 3
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            {errorMessage && (
              <p className='text-destructive text-sm'>{errorMessage}</p>
            )}

            <div className='flex justify-end gap-3 pt-2'>
              <Button
                type='button'
                variant='outline'
                onClick={() => navigate({ to: '/sites' })}
              >
                Cancel
              </Button>
              <Button
                type='submit'
                disabled={mutation.isPending || !serverID}
              >
                {mutation.isPending ? 'Adding…' : 'Add site'}
              </Button>
            </div>
          </form>
        </div>
      </Main>
    </>
  )
}
