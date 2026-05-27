import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { isApiError } from '@/lib/api'
import { createServer, type CreateServerInput } from './api'

export function NewServerForm() {
  const navigate = useNavigate()
  const qc = useQueryClient()

  const [name, setName] = useState('')
  const [publicIP, setPublicIP] = useState('')
  const [sshUser, setSSHUser] = useState('root')
  const [sshPort, setSSHPort] = useState('22')
  const [authMode, setAuthMode] = useState<'password' | 'key'>('password')
  const [password, setPassword] = useState('')
  const [privateKey, setPrivateKey] = useState('')

  const mutation = useMutation({
    mutationFn: (input: CreateServerInput) => createServer(input),
    onSuccess: async (created) => {
      await qc.invalidateQueries({ queryKey: ['servers'] })
      await navigate({
        to: '/servers/$id',
        params: { id: String(created.id) },
      })
    },
  })

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const port = Number(sshPort) || 22
    mutation.mutate({
      name: name.trim(),
      public_ip: publicIP.trim(),
      ssh_user: sshUser.trim(),
      ssh_port: port,
      ssh_password: authMode === 'password' ? password : undefined,
      ssh_private_key: authMode === 'key' ? privateKey : undefined,
    })
  }

  const errorMessage =
    mutation.error && isApiError(mutation.error)
      ? mutation.error.code
      : mutation.error
        ? 'Failed to add server'
        : null

  return (
    <>
      <Header />
      <Main>
        <div className='mx-auto max-w-xl'>
          <h1 className='mb-1 text-2xl font-semibold'>Add server</h1>
          <p className='text-muted-foreground mb-6 text-sm'>
            Aegis will SSH in, install the agent, and bring the host online.
            Credentials are used only for this single bootstrap.
          </p>

          <form className='space-y-4' onSubmit={onSubmit}>
            <div className='grid gap-2'>
              <Label htmlFor='name'>Name</Label>
              <Input
                id='name'
                placeholder='production-1'
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>

            <div className='grid grid-cols-2 gap-4'>
              <div className='grid gap-2'>
                <Label htmlFor='ip'>Public IP</Label>
                <Input
                  id='ip'
                  placeholder='203.0.113.10'
                  value={publicIP}
                  onChange={(e) => setPublicIP(e.target.value)}
                  required
                />
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='port'>SSH port</Label>
                <Input
                  id='port'
                  type='number'
                  placeholder='22'
                  value={sshPort}
                  onChange={(e) => setSSHPort(e.target.value)}
                />
              </div>
            </div>

            <div className='grid gap-2'>
              <Label htmlFor='user'>SSH user</Label>
              <Input
                id='user'
                placeholder='root'
                value={sshUser}
                onChange={(e) => setSSHUser(e.target.value)}
                required
              />
            </div>

            <Tabs
              value={authMode}
              onValueChange={(v) => setAuthMode(v as 'password' | 'key')}
            >
              <TabsList className='grid w-full grid-cols-2'>
                <TabsTrigger value='password'>Password</TabsTrigger>
                <TabsTrigger value='key'>Private key</TabsTrigger>
              </TabsList>
              <TabsContent value='password' className='mt-4'>
                <div className='grid gap-2'>
                  <Label htmlFor='pw'>SSH password</Label>
                  <Input
                    id='pw'
                    type='password'
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </div>
              </TabsContent>
              <TabsContent value='key' className='mt-4'>
                <div className='grid gap-2'>
                  <Label htmlFor='key'>Private key (PEM)</Label>
                  <Textarea
                    id='key'
                    rows={8}
                    placeholder='-----BEGIN OPENSSH PRIVATE KEY-----'
                    className='font-mono text-sm'
                    value={privateKey}
                    onChange={(e) => setPrivateKey(e.target.value)}
                  />
                </div>
              </TabsContent>
            </Tabs>

            {errorMessage && (
              <p className='text-destructive text-sm'>{errorMessage}</p>
            )}

            <div className='flex justify-end gap-3 pt-2'>
              <Button
                type='button'
                variant='outline'
                onClick={() => navigate({ to: '/servers' })}
              >
                Cancel
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? 'Adding…' : 'Add server'}
              </Button>
            </div>
          </form>
        </div>
      </Main>
    </>
  )
}
