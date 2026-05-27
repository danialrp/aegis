import { Badge } from '@/components/ui/badge'
import { type SiteStatus } from './api'

const STATUS_VARIANT: Record<
  SiteStatus,
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
  pending: 'secondary',
  provisioning: 'secondary',
  ready: 'default',
  error: 'destructive',
}

const STATUS_LABEL: Record<SiteStatus, string> = {
  pending: 'Pending',
  provisioning: 'Provisioning…',
  ready: 'Ready',
  error: 'Error',
}

export function SiteStatusBadge({ status }: { status: SiteStatus }) {
  return <Badge variant={STATUS_VARIANT[status]}>{STATUS_LABEL[status]}</Badge>
}
