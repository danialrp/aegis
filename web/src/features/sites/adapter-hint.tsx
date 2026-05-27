import { Server } from 'lucide-react'
import { type SiteType } from './api'

// AdapterHint surfaces what Aegis has provisioned host-side for a
// given site type, in language an operator wants to see on first
// look. Renders nothing for site types where the existing panels
// already say what's going on (static / docker).
export function AdapterHint({
  siteType,
  proxyPort,
}: {
  siteType: SiteType
  proxyPort?: number | null
}) {
  const text = adapterCopy(siteType, proxyPort ?? null)
  if (!text) return null
  return (
    <div className='border-muted bg-muted/30 flex gap-3 rounded-md border p-3 text-sm'>
      <Server className='text-muted-foreground mt-0.5 h-4 w-4 shrink-0' />
      <div className='space-y-1'>
        <p className='font-medium'>{text.title}</p>
        <p className='text-muted-foreground text-xs'>{text.detail}</p>
      </div>
    </div>
  )
}

function adapterCopy(
  siteType: SiteType,
  proxyPort: number | null
): { title: string; detail: string } | null {
  switch (siteType) {
    case 'laravel':
      return {
        title: 'Laravel adapter — PHP-FPM pool running as this site',
        detail:
          'Per-site PHP-FPM pool on /run/php/aegis-site-<id>.sock; nginx fastcgi to it. The default deploy script runs composer install + artisan migrate/cache.',
      }
    case 'wordpress':
      return {
        title: 'WordPress adapter — PHP-FPM pool running as this site',
        detail:
          'Same PHP-FPM stack as Laravel. The default deploy script downloads WP core on first run and updates it via wp-cli thereafter. DB provisioning lands in Phase 5; for now, fill wp-config.php with your own credentials.',
      }
    case 'php':
      return {
        title: 'Generic PHP adapter',
        detail:
          'Per-site PHP-FPM pool + fastcgi nginx vhost. Bring your own framework — the deploy script defaults to `composer install` when a composer.json is present.',
      }
    case 'nextjs':
      return {
        title: 'Next.js adapter — Node + supervisor + nginx proxy',
        detail: `nginx proxies ${proxyPort ? `127.0.0.1:${proxyPort}` : 'the upstream port'} to a Next.js server you run via a Daemon (Daemons panel below). Run \`npm run build\` from the deploy script, then restart the daemon.`,
      }
    default:
      return null
  }
}
