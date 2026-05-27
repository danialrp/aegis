import { createFileRoute } from '@tanstack/react-router'
import { NewSiteForm } from '@/features/sites/new'

export const Route = createFileRoute('/_authenticated/sites/new')({
  component: NewSiteForm,
})
