import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, UserPlus, Users } from 'lucide-react'
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
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import {
  addTeamMember,
  createTeam,
  deleteTeam,
  listTeamMembers,
  listTeams,
  listUsers,
  removeTeamMember,
  type Team,
} from './api'

export function TeamsPage() {
  const qc = useQueryClient()
  const { data, isLoading, error } = useQuery({
    queryKey: ['teams'],
    queryFn: listTeams,
  })
  const remove = useMutation({
    mutationFn: (id: number) => deleteTeam(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['teams'] }),
  })

  return (
    <>
      <Header />
      <Main>
        <div className='mb-6 flex items-center justify-between'>
          <div>
            <h1 className='text-2xl font-semibold'>Teams</h1>
            <p className='text-muted-foreground text-sm'>
              Group users so per-site permissions can be assigned to the
              group rather than each operator individually.
            </p>
          </div>
          <NewTeamDialog
            onCreated={() => qc.invalidateQueries({ queryKey: ['teams'] })}
          />
        </div>

        {isLoading && <p className='text-muted-foreground'>Loading…</p>}
        {error && <p className='text-destructive'>Failed to load teams.</p>}

        {!isLoading && (data?.length ?? 0) === 0 && (
          <div className='rounded-md border border-dashed p-10 text-center'>
            <Users className='text-muted-foreground mx-auto mb-3 h-6 w-6' />
            <h2 className='text-lg font-medium'>No teams yet</h2>
            <p className='text-muted-foreground text-sm'>
              Create one to start grouping operators by access level.
            </p>
          </div>
        )}

        {!isLoading && (data?.length ?? 0) > 0 && (
          <div className='space-y-3'>
            {data?.map((t) => (
              <TeamCard
                key={t.id}
                team={t}
                onDelete={() => {
                  if (window.confirm(`Delete team "${t.name}"?`))
                    remove.mutate(t.id)
                }}
              />
            ))}
          </div>
        )}
      </Main>
    </>
  )
}

function TeamCard({ team, onDelete }: { team: Team; onDelete: () => void }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)

  const { data: members } = useQuery({
    queryKey: ['team-members', team.id],
    queryFn: () => listTeamMembers(team.id),
  })
  const removeMem = useMutation({
    mutationFn: (uid: number) => removeTeamMember(team.id, uid),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ['team-members', team.id] }),
  })

  return (
    <div className='border-border rounded-md border p-4'>
      <div className='flex items-start justify-between'>
        <div>
          <div className='flex items-center gap-2'>
            <Users className='h-4 w-4' />
            <p className='font-medium'>{team.name}</p>
            <Badge variant='outline' className='text-xs'>
              {members?.length ?? 0} member
              {members?.length === 1 ? '' : 's'}
            </Badge>
          </div>
          {team.description && (
            <p className='text-muted-foreground mt-1 text-sm'>
              {team.description}
            </p>
          )}
        </div>
        <div className='flex items-center gap-2'>
          <Button variant='outline' onClick={() => setOpen(true)}>
            <UserPlus className='mr-2 h-4 w-4' />
            Add member
          </Button>
          <Button variant='destructive' size='icon' onClick={onDelete}>
            <Trash2 className='h-4 w-4' />
          </Button>
        </div>
      </div>

      {(members?.length ?? 0) > 0 && (
        <div className='mt-3 space-y-1'>
          {members?.map((m) => (
            <div
              key={m.user_id}
              className='border-border flex items-center justify-between rounded-md border px-3 py-2 text-sm'
            >
              <div>
                <span className='font-medium'>{m.email}</span>
                <Badge variant='outline' className='ml-2 text-xs'>
                  {m.role_in_team}
                </Badge>
              </div>
              <Button
                size='icon'
                variant='ghost'
                onClick={() => removeMem.mutate(m.user_id)}
                title='Remove from team'
              >
                <Trash2 className='h-3 w-3' />
              </Button>
            </div>
          ))}
        </div>
      )}

      <AddMemberDialog
        teamID={team.id}
        open={open}
        onOpenChange={setOpen}
        onAdded={() =>
          qc.invalidateQueries({ queryKey: ['team-members', team.id] })
        }
      />
    </div>
  )
}

function NewTeamDialog({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const create = useMutation({
    mutationFn: () => createTeam(name.trim(), desc || undefined),
    onSuccess: () => {
      onCreated()
      setOpen(false)
      setName('')
      setDesc('')
    },
  })
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus className='mr-2 h-4 w-4' />
          New team
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New team</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label htmlFor='name'>Name</Label>
            <Input
              id='name'
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder='Devs'
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='desc'>Description (optional)</Label>
            <Input
              id='desc'
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button disabled={!name || create.isPending} onClick={() => create.mutate()}>
            {create.isPending ? 'Creating…' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AddMemberDialog({
  teamID,
  open,
  onOpenChange,
  onAdded,
}: {
  teamID: number
  open: boolean
  onOpenChange: (v: boolean) => void
  onAdded: () => void
}) {
  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: listUsers,
    enabled: open,
  })
  const [uid, setUid] = useState<string>('')
  const [role, setRole] = useState<'owner' | 'member'>('member')

  const add = useMutation({
    mutationFn: () => addTeamMember(teamID, Number(uid), role),
    onSuccess: () => {
      onAdded()
      onOpenChange(false)
      setUid('')
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add member</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='grid gap-2'>
            <Label>User</Label>
            <Select value={uid} onValueChange={setUid}>
              <SelectTrigger>
                <SelectValue placeholder='Pick a user' />
              </SelectTrigger>
              <SelectContent>
                {users?.map((u) => (
                  <SelectItem key={u.id} value={String(u.id)}>
                    {u.email}
                    <span className='text-muted-foreground ml-2 text-xs'>
                      ({u.role})
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label>Role in team</Label>
            <Select
              value={role}
              onValueChange={(v) => setRole(v as 'owner' | 'member')}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='member'>member</SelectItem>
                <SelectItem value='owner'>owner</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={!uid || add.isPending} onClick={() => add.mutate()}>
            {add.isPending ? 'Adding…' : 'Add'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
