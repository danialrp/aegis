import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { getDeployScript, putDeployScript } from './deploys-api'

// DeployScriptEditor — Phase 1.4 plain-textarea editor + Phase 1.7
// cron field. Monaco can drop in later without changing this
// component's prop surface.
export function DeployScriptEditor({ siteID }: { siteID: number }) {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['deploy-script', siteID],
    queryFn: () => getDeployScript(siteID),
  })

  // Derive editor state from server data unless the operator has
  // typed something; dirty is implicit in (draftBody | draftCron) !== null.
  const [draftBody, setDraftBody] = useState<string | null>(null)
  const [draftCron, setDraftCron] = useState<string | null>(null)
  const body = draftBody ?? data?.body ?? ''
  const cron = draftCron ?? data?.cron_spec ?? ''
  const dirty = draftBody !== null || draftCron !== null

  const save = useMutation({
    mutationFn: () =>
      putDeployScript(siteID, body, cron.trim() === '' ? null : cron.trim()),
    onSuccess: async () => {
      setDraftBody(null)
      setDraftCron(null)
      await qc.invalidateQueries({ queryKey: ['deploy-script', siteID] })
    },
  })

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <h2 className='text-lg font-medium'>Deploy script</h2>
        <div className='text-muted-foreground text-xs'>
          {data && data.updated_at ? (
            <>Last saved: {new Date(data.updated_at).toLocaleString()}</>
          ) : (
            <>Never saved</>
          )}
        </div>
      </div>
      <p className='text-muted-foreground text-sm'>
        Free-form bash. Runs as the site's Linux user, with cwd set to
        the site's working directory.
      </p>
      {isLoading ? (
        <p className='text-muted-foreground text-sm'>Loading…</p>
      ) : (
        <Textarea
          value={body}
          onChange={(e) => setDraftBody(e.target.value)}
          rows={18}
          className='font-mono text-sm'
        />
      )}

      <div className='grid gap-2'>
        <Label htmlFor='cron'>Schedule (cron, optional)</Label>
        <Input
          id='cron'
          placeholder='e.g. 0 3 * * *  (daily at 03:00 UTC)'
          value={cron}
          onChange={(e) => setDraftCron(e.target.value)}
          className='font-mono text-sm'
        />
        <p className='text-muted-foreground text-xs'>
          Standard 5-field cron. Empty = no schedule (manual + webhook
          only). Re-syncs from the DB every 30s.
        </p>
      </div>

      <div className='flex justify-end gap-2'>
        <Button
          variant='outline'
          disabled={!dirty}
          onClick={() => {
            setDraftBody(null)
            setDraftCron(null)
          }}
        >
          Revert
        </Button>
        <Button
          disabled={!dirty || save.isPending}
          onClick={() => save.mutate()}
        >
          {save.isPending ? 'Saving…' : 'Save script'}
        </Button>
      </div>
    </div>
  )
}
