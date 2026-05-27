import { createFileRoute } from '@tanstack/react-router'
import { SitesList } from '@/features/sites/list'

export const Route = createFileRoute('/_authenticated/sites/')({
  component: SitesList,
})
