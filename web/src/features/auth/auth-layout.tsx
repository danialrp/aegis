import { Logo } from '@/assets/logo'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  return (
    <div className='relative flex min-h-svh items-center justify-center overflow-hidden p-6'>
      {/* Soft radial glow behind the card — gives the page a sense of
          depth without leaning on a decorative image. */}
      <div
        className='pointer-events-none absolute inset-0 -z-10'
        style={{
          background:
            'radial-gradient(80% 60% at 50% 20%, rgba(0, 167, 111, 0.12), transparent 70%), radial-gradient(60% 40% at 50% 100%, rgba(0, 184, 217, 0.08), transparent 70%)',
        }}
      />
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-6'>
        <div className='flex items-center gap-2'>
          <Logo className='size-7' />
          <h1 className='text-xl font-bold tracking-tight'>Aegis</h1>
        </div>
        {children}
      </div>
    </div>
  )
}
