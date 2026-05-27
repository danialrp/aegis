import { createFileRoute } from '@tanstack/react-router'
import { DeployDetail } from '@/features/deploys/detail'

export const Route = createFileRoute('/_authenticated/deploys/$deployId')({
  component: function DeployDetailRoute() {
    const { deployId } = Route.useParams()
    return <DeployDetail deployID={Number(deployId)} />
  },
})
