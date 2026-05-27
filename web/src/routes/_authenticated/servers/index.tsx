import { createFileRoute } from '@tanstack/react-router'
import { ServersList } from '@/features/servers/list'

export const Route = createFileRoute('/_authenticated/servers/')({
  component: ServersList,
})
