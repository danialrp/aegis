import { Globe, LayoutDashboard, Server } from 'lucide-react'
import { type SidebarData } from '../types'

// Aegis sidebar — intentionally minimal in Phase 0. New nav groups are
// added as features land: Sites in Phase 1, Databases in Phase 5, etc.
export const sidebarData: SidebarData = {
  user: {
    name: '',
    email: '',
    avatar: '',
  },
  teams: [],
  navGroups: [
    {
      title: 'Overview',
      items: [
        {
          title: 'Dashboard',
          url: '/',
          icon: LayoutDashboard,
        },
        {
          title: 'Servers',
          url: '/servers',
          icon: Server,
        },
        {
          title: 'Sites',
          url: '/sites',
          icon: Globe,
        },
      ],
    },
  ],
}
