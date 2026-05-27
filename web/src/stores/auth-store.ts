// Aegis auth state.
//
// Tokens are persisted in cookies so they survive page reloads. The
// refresh token is the server-side session UUID (opaque); the access
// token is a short-lived JWT. Neither is httpOnly — XSS would expose
// both. Migration to httpOnly cookies is tracked for v1.0.
//
// See SECURITY.md and the 0.5/0.6 design notes for the rationale.
import { create } from 'zustand'
import { getCookie, removeCookie, setCookie } from '@/lib/cookies'

const ACCESS_COOKIE = 'aegis_access'
const REFRESH_COOKIE = 'aegis_refresh'
const USER_COOKIE = 'aegis_user'

export interface AuthUser {
  id: number
  email: string
  role: string
  enabled: boolean
}

interface AuthState {
  auth: {
    user: AuthUser | null
    accessToken: string
    refreshToken: string
    setUser: (user: AuthUser | null) => void
    setTokens: (access: string, refresh: string, expiresAt: string) => void
    reset: () => void
  }
}

function readJSON<T>(name: string): T | null {
  const raw = getCookie(name)
  if (!raw) return null
  try {
    return JSON.parse(raw) as T
  } catch {
    return null
  }
}

function readString(name: string): string {
  const raw = getCookie(name)
  if (!raw) return ''
  try {
    return JSON.parse(raw) as string
  } catch {
    return raw
  }
}

export const useAuthStore = create<AuthState>()((set) => ({
  auth: {
    user: readJSON<AuthUser>(USER_COOKIE),
    accessToken: readString(ACCESS_COOKIE),
    refreshToken: readString(REFRESH_COOKIE),

    setUser: (user) =>
      set((state) => {
        if (user) {
          setCookie(USER_COOKIE, JSON.stringify(user))
        } else {
          removeCookie(USER_COOKIE)
        }
        return { ...state, auth: { ...state.auth, user } }
      }),

    setTokens: (access, refresh, _expiresAt) =>
      set((state) => {
        setCookie(ACCESS_COOKIE, JSON.stringify(access))
        setCookie(REFRESH_COOKIE, JSON.stringify(refresh))
        return {
          ...state,
          auth: {
            ...state.auth,
            accessToken: access,
            refreshToken: refresh,
          },
        }
      }),

    reset: () =>
      set((state) => {
        removeCookie(ACCESS_COOKIE)
        removeCookie(REFRESH_COOKIE)
        removeCookie(USER_COOKIE)
        return {
          ...state,
          auth: {
            ...state.auth,
            user: null,
            accessToken: '',
            refreshToken: '',
          },
        }
      }),
  },
}))
