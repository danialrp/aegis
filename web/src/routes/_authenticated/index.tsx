import { createFileRoute } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

export const Route = createFileRoute('/_authenticated/')({
  component: Dashboard,
})

function Dashboard() {
  const { auth } = useAuthStore()
  return (
    <div className='p-6'>
      <Card>
        <CardHeader>
          <CardTitle>Welcome to Aegis</CardTitle>
          <CardDescription>
            Signed in as <span className='font-medium'>{auth.user?.email}</span>{' '}
            ({auth.user?.role}).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className='text-sm text-muted-foreground'>
            Server provisioning, sites, and deploy scripts will land here as
            Phase 1 progresses.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
