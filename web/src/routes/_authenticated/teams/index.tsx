import { createFileRoute } from '@tanstack/react-router'
import { TeamsPage } from '@/features/access/teams-page'

export const Route = createFileRoute('/_authenticated/teams/')({
  component: TeamsPage,
})
