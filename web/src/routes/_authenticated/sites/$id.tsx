import { createFileRoute } from '@tanstack/react-router'
import { SiteDetail } from '@/features/sites/detail'

export const Route = createFileRoute('/_authenticated/sites/$id')({
  component: function SiteDetailRoute() {
    const { id } = Route.useParams()
    return <SiteDetail id={Number(id)} />
  },
})
