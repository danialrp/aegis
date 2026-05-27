import { toast } from 'sonner'
import { isApiError } from '@/lib/api'

// Aegis fetches via the `apiFetch` wrapper in lib/api.ts, which throws
// ApiError values. TanStack Query's global error hook calls into here.
export function handleServerError(error: unknown) {
  if (import.meta.env.DEV) {
    // eslint-disable-next-line no-console
    console.log(error)
  }

  let errMsg = 'Something went wrong.'

  if (isApiError(error)) {
    if (error.code === 'rate_limited') {
      errMsg = 'Too many requests — please wait and try again.'
    } else if (error.status === 401) {
      errMsg = 'Your session has expired. Please sign in again.'
    } else if (error.status === 403) {
      errMsg = 'You do not have permission to perform that action.'
    } else if (error.status >= 500) {
      errMsg = 'Server error. Please try again.'
    } else if (error.message) {
      errMsg = error.message
    }
  }

  toast.error(errMsg)
}
