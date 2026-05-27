import { useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, LogIn } from 'lucide-react'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { apiFetch, isApiError } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/password-input'

const formSchema = z.object({
  email: z.email({
    error: (iss) => (iss.input === '' ? 'Please enter your email.' : undefined),
  }),
  password: z
    .string()
    .min(1, 'Please enter your password.')
    .min(12, 'Password must be at least 12 characters long.'),
})

type LoginResponse = {
  access_token: string
  refresh_token: string
  expires_at: string
}

type MeResponse = {
  id: number
  email: string
  role: string
  enabled: boolean
}

interface UserAuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}

export function UserAuthForm({
  className,
  redirectTo,
  ...props
}: UserAuthFormProps) {
  const [isLoading, setIsLoading] = useState(false)
  const navigate = useNavigate()
  const { auth } = useAuthStore()

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: { email: '', password: '' },
  })

  async function onSubmit(data: z.infer<typeof formSchema>) {
    setIsLoading(true)
    try {
      const tokens = await apiFetch<LoginResponse>('/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify(data),
      })
      auth.setTokens(
        tokens.access_token,
        tokens.refresh_token,
        tokens.expires_at
      )

      const me = await apiFetch<MeResponse>('/v1/auth/me')
      auth.setUser(me)

      toast.success(`Welcome back, ${me.email}`)
      navigate({ to: redirectTo || '/', replace: true })
    } catch (err) {
      if (isApiError(err) && err.code === 'invalid_credentials') {
        toast.error('Invalid email or password.')
      } else if (isApiError(err) && err.code === 'account_disabled') {
        toast.error('This account is disabled.')
      } else if (isApiError(err) && err.code === 'rate_limited') {
        toast.error('Too many attempts — please wait and try again.')
      } else {
        toast.error('Sign-in failed. Please try again.')
      }
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className={cn('grid gap-3', className)}
        {...props}
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel>Email</FormLabel>
              <FormControl>
                <Input
                  placeholder='you@example.com'
                  autoComplete='email'
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>Password</FormLabel>
              <FormControl>
                <PasswordInput
                  placeholder='********'
                  autoComplete='current-password'
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <Button type='submit' className='mt-2' disabled={isLoading}>
          {isLoading ? (
            <Loader2 className='animate-spin' />
          ) : (
            <LogIn className='mr-2' />
          )}
          Sign in
        </Button>
      </form>
    </Form>
  )
}
