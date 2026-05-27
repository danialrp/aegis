import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { getCompose, putCompose } from './docker-api'

export function ComposeEditor({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['compose', siteID],
    queryFn: () => getCompose(siteID),
  })

  // Derive the editor value from server data unless the operator has
  // typed something. Avoids the "load remote into local state via
  // useEffect" anti-pattern: dirty is implicit in draft !== null.
  const [draft, setDraft] = useState<string | null>(null)
  const body = draft ?? data?.body ?? ''
  const dirty = draft !== null

  const save = useMutation({
    mutationFn: () => putCompose(siteID, body),
    onSuccess: async () => {
      setDraft(null)
      await qc.invalidateQueries({ queryKey: ['compose', siteID] })
    },
  })

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Compose file</h2>
          <p className='text-muted-foreground text-sm'>
            Saved to <span className='font-mono'>/srv/sites/{siteID}/compose.yml</span>.
            Aegis runs it with project name{' '}
            <span className='font-mono'>aegis-site-{siteID}</span>.
          </p>
        </div>
        <div className='text-muted-foreground text-xs'>
          {data && data.updated_at ? (
            <>Last saved: {new Date(data.updated_at).toLocaleString()}</>
          ) : (
            <>Never saved</>
          )}
        </div>
      </div>
      {isLoading ? (
        <p className='text-muted-foreground text-sm'>Loading…</p>
      ) : (
        <Textarea
          value={body}
          onChange={(e) => setDraft(e.target.value)}
          rows={16}
          className='font-mono text-sm'
        />
      )}
      <div className='flex justify-end gap-2'>
        <Button
          variant='outline'
          disabled={!dirty}
          onClick={() => setDraft(null)}
        >
          Revert
        </Button>
        <Button
          disabled={!dirty || save.isPending}
          onClick={() => save.mutate()}
        >
          {save.isPending ? 'Saving…' : 'Save compose'}
        </Button>
      </div>
    </div>
  )
}
