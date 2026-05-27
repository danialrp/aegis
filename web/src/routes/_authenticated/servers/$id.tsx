import { createFileRoute } from '@tanstack/react-router'
import { ServerDetail } from '@/features/servers/detail'

export const Route = createFileRoute('/_authenticated/servers/$id')({
  component: function ServerDetailRoute() {
    const { id } = Route.useParams()
    return <ServerDetail id={Number(id)} />
  },
})
