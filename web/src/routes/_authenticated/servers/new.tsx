import { createFileRoute } from '@tanstack/react-router'
import { NewServerForm } from '@/features/servers/new'

export const Route = createFileRoute('/_authenticated/servers/new')({
  component: NewServerForm,
})
