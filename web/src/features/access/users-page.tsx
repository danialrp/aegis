import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Switch,
} from '@/components/ui/switch'
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
import {
  createUser,
  deleteUser,
  listUsers,
  updateUser,
  type UserRole,
} from './api'

export function UsersPage() {
  const qc = useQueryClient()
  const { data, isLoading, error } = useQuery({
    queryKey: ['users'],
    queryFn: listUsers,
  })

  const remove = useMutation({
    mutationFn: (id: number) => deleteUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      updateUser(id, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['users'] }),
  })

  return (
    <>
      <Header />
      <Main>
        <div className='mb-6 flex items-center justify-between'>
          <div>
            <h1 className='text-2xl font-semibold'>Users</h1>
            <p className='text-muted-foreground text-sm'>
              Operators with access to the panel. Per-site capabilities
              are assigned via team or per-user permissions on the site
              detail page.
            </p>
          </div>
          <NewUserDialog
            onCreated={() => qc.invalidateQueries({ queryKey: ['users'] })}
          />
        </div>

        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && <p className='text-destructive'>Failed to load users.</p>}

        {!isLoading && data && (
          <div className='rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Email</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Enabled</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className='w-12'></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className='font-medium'>{u.email}</TableCell>
                    <TableCell>
                      <Badge variant={roleVariant(u.role)}>{u.role}</Badge>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={u.enabled}
                        disabled={u.role === 'god'}
                        onCheckedChange={(v) =>
                          toggle.mutate({ id: u.id, enabled: v })
                        }
                      />
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {new Date(u.created_at).toLocaleDateString()}
                    </TableCell>
                    <TableCell>
                      <Button
                        size='icon'
                        variant='ghost'
                        disabled={u.role === 'god'}
                        onClick={() => {
                          if (
                            window.confirm(`Delete user ${u.email}?`)
                          )
                            remove.mutate(u.id)
                        }}
                        title={
                          u.role === 'god' ? 'god cannot be deleted' : 'Delete'
                        }
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
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

function NewUserDialog({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<UserRole>('site_user')
  const [enabled, setEnabled] = useState(true)
  const [err, setErr] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: () =>
      createUser({ email: email.trim(), password, role, enabled }),
    onSuccess: () => {
      onCreated()
      setOpen(false)
      setEmail('')
      setPassword('')
      setRole('site_user')
      setErr(null)
    },
    onError: (e: unknown) => {
      const code =
        typeof e === 'object' && e && 'code' in e
          ? (e as { code?: string }).code
          : 'failed'
      setErr(code ?? 'failed')
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className='mr-2 h-4 w-4' />
          New user
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New user</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label htmlFor='email'>Email</Label>
            <Input
              id='email'
              type='email'
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder='you@example.com'
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='password'>Password</Label>
            <Input
              id='password'
              type='password'
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder='at least 12 chars'
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='role'>Role</Label>
            <Select value={role} onValueChange={(v) => setRole(v as UserRole)}>
              <SelectTrigger id='role'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='site_user'>site_user</SelectItem>
                <SelectItem value='admin'>admin (god only)</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className='flex items-center justify-between'>
            <Label htmlFor='enabled'>Enabled</Label>
            <Switch
              id='enabled'
              checked={enabled}
              onCheckedChange={setEnabled}
            />
          </div>
          {err && <p className='text-destructive text-sm'>{err}</p>}
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            disabled={!email || !password || create.isPending}
            onClick={() => create.mutate()}
          >
            {create.isPending ? 'Creating…' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function roleVariant(
  role: string
): 'default' | 'secondary' | 'destructive' | 'outline' {
  switch (role) {
    case 'god':
      return 'destructive'
    case 'admin':
      return 'default'
    default:
      return 'secondary'
  }
}
