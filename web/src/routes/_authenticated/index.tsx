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
      <Card className='shadow-md'>
        <CardHeader>
          <CardTitle className='text-2xl font-bold tracking-tight'>
            Welcome to Aegis
          </CardTitle>
          <CardDescription>
            Signed in as <span className='font-medium'>{auth.user?.email}</span>{' '}
            ({auth.user?.role}).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className='text-muted-foreground text-sm'>
            Manage your servers, sites, and deploy scripts from the navigation
            on the left.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
