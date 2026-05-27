import { useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTermTerminal } from '@xterm/xterm'
import { PowerOff, Terminal } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ApiError, isApiError } from '@/lib/api'
import { issueTerminalTicket } from './terminal-api'
import '@xterm/xterm/css/xterm.css'

type ConnState = 'idle' | 'connecting' | 'connected' | 'closed' | 'error'

export function TerminalPanel({ siteID }: { siteID: number }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<XTermTerminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const [state, setState] = useState<ConnState>('idle')
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  const cleanup = () => {
    wsRef.current?.close()
    wsRef.current = null
    termRef.current?.dispose()
    termRef.current = null
    fitRef.current = null
  }

  useEffect(() => () => cleanup(), [])

  const start = async () => {
    if (state === 'connecting' || state === 'connected') return
    setErrorMsg(null)
    setState('connecting')

    let ticket: string
    try {
      const t = await issueTerminalTicket(siteID)
      ticket = t.ticket
    } catch (err) {
      const code = isApiError(err) ? (err as ApiError).code : 'unknown'
      setErrorMsg(
        code === 'agent_offline'
          ? 'Agent is not connected — terminal is unavailable.'
          : `Ticket request failed: ${code}`
      )
      setState('error')
      return
    }

    if (!containerRef.current) return
    const term = new XTermTerminal({
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: '#0a0a0a',
        foreground: '#e5e7eb',
        cursor: '#e5e7eb',
      },
      cursorBlink: true,
      convertEol: true,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${scheme}://${window.location.host}/v1/sites/${siteID}/terminal?ticket=${encodeURIComponent(ticket)}`
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => setState('connected')
    ws.onerror = () => {
      setErrorMsg('WebSocket error.')
      setState('error')
    }
    ws.onclose = () => {
      setState((s) => (s === 'error' ? s : 'closed'))
    }
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data as string)
        if (typeof msg.data === 'string') {
          const bytes = atob(msg.data)
          term.write(bytes)
        }
      } catch {
        // Ignore malformed frames; agent is well-behaved.
      }
    }

    term.onData((data) => {
      if (ws.readyState !== WebSocket.OPEN) return
      const encoded = btoa(
        Array.from(new TextEncoder().encode(data))
          .map((b) => String.fromCharCode(b))
          .join('')
      )
      ws.send(JSON.stringify({ data: encoded }))
    })
  }

  const stop = () => {
    cleanup()
    setState('idle')
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <div>
          <h2 className='text-lg font-medium'>Terminal</h2>
          <p className='text-muted-foreground text-sm'>
            Browser shell into <span className='font-mono'>site_{siteID}</span>{' '}
            on the managed host. Closes when the tab does.
          </p>
        </div>
        <div className='flex items-center gap-2'>
          {state === 'connected' || state === 'connecting' ? (
            <Button variant='outline' onClick={stop}>
              <PowerOff className='mr-2 h-4 w-4' />
              Disconnect
            </Button>
          ) : (
            <Button onClick={start}>
              <Terminal className='mr-2 h-4 w-4' />
              {state === 'closed' ? 'Reconnect' : 'Open terminal'}
            </Button>
          )}
        </div>
      </div>

      {errorMsg && (
        <p className='text-destructive text-sm'>{errorMsg}</p>
      )}

      {state === 'idle' ? (
        <div className='border-muted bg-muted/30 rounded-md border border-dashed p-6 text-center text-sm'>
          Click <b>Open terminal</b> to spawn a shell as{' '}
          <span className='font-mono'>site_{siteID}</span>.
        </div>
      ) : (
        <div
          ref={containerRef}
          className='border-border h-96 rounded-md border bg-[#0a0a0a] p-2'
        />
      )}

      {state === 'closed' && !errorMsg && (
        <p className='text-muted-foreground text-xs'>
          Disconnected.
        </p>
      )}
    </div>
  )
}
