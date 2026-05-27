import { type QueryClient } from '@tanstack/react-query'
import {
  createRootRouteWithContext,
  Link,
  Outlet,
} from '@tanstack/react-router'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'
import { Toaster } from '@/components/ui/sonner'
import { NavigationProgress } from '@/components/navigation-progress'

function NotFound() {
  return (
    <div className='flex min-h-svh flex-col items-center justify-center gap-3 p-6 text-center'>
      <h1 className='text-3xl font-semibold tracking-tight'>404</h1>
      <p className='text-muted-foreground'>
        The page you were looking for doesn't exist.
      </p>
      <Link to='/' className='text-sm underline underline-offset-4'>
        Back to dashboard
      </Link>
    </div>
  )
}

function GeneralError({ error }: { error: Error }) {
  return (
    <div className='flex min-h-svh flex-col items-center justify-center gap-3 p-6 text-center'>
      <h1 className='text-3xl font-semibold tracking-tight'>
        Something went wrong
      </h1>
      <p className='text-muted-foreground'>{error.message}</p>
      <Link to='/' className='text-sm underline underline-offset-4'>
        Back to dashboard
      </Link>
    </div>
  )
}

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
}>()({
  component: () => {
    return (
      <>
        <NavigationProgress />
        <Outlet />
        <Toaster duration={5000} />
        {import.meta.env.MODE === 'development' && (
          <>
            <ReactQueryDevtools buttonPosition='bottom-left' />
            <TanStackRouterDevtools position='bottom-right' />
          </>
        )}
      </>
    )
  },
  notFoundComponent: NotFound,
  errorComponent: GeneralError,
})
