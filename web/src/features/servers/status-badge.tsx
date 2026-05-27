import { Badge } from '@/components/ui/badge'
import { type ServerStatus } from './api'

const STATUS_VARIANT: Record<
  ServerStatus,
  'default' | 'secondary' | 'destructive' | 'outline'
> = {
  pending: 'secondary',
  provisioning: 'secondary',
  ready: 'default',
  error: 'destructive',
}

const STATUS_LABEL: Record<ServerStatus, string> = {
  pending: 'Pending',
  provisioning: 'Provisioning…',
  ready: 'Ready',
  error: 'Error',
}

export function StatusBadge({ status }: { status: ServerStatus }) {
  return <Badge variant={STATUS_VARIANT[status]}>{STATUS_LABEL[status]}</Badge>
}
