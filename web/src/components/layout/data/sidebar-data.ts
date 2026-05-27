import { Globe, LayoutDashboard, Server, User, Users } from 'lucide-react'
import { type SidebarData } from '../types'

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
    {
      title: 'Access',
      items: [
        {
          title: 'Users',
          url: '/users',
          icon: User,
        },
        {
          title: 'Teams',
          url: '/teams',
          icon: Users,
        },
      ],
    },
  ],
}
