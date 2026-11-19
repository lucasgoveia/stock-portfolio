import { useQuery } from '@tanstack/react-query'
import { Link, useRouterState } from '@tanstack/react-router'
import {
  Bell,
  CalendarClock,
  Eye,
  LayoutDashboard,
  LineChart,
  Receipt,
  Settings,
  TrendingUp,
  Wallet,
  type LucideIcon,
} from 'lucide-react'

import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { pendingReviewQuery } from '@/lib/queries'

const items: { to: string; label: string; icon: LucideIcon }[] = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/alerts', label: 'Alerts', icon: Bell },
  { to: '/watchlist', label: 'Watchlist', icon: Eye },
  { to: '/assets', label: 'Assets', icon: TrendingUp },
  { to: '/contributions', label: 'Contributions', icon: CalendarClock },
  { to: '/trades', label: 'Trades & cash', icon: Wallet },
  { to: '/taxes', label: 'Taxes', icon: Receipt },
  { to: '/performance', label: 'Performance', icon: LineChart },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function AppSidebar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { setOpenMobile } = useSidebar()
  const review = useQuery(pendingReviewQuery)
  const pending = review.data?.total ?? 0
  return (
    <Sidebar>
      <SidebarHeader className="display px-4 py-3 text-lg font-semibold">Stock Portfolio</SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {items.map(({ to, label, icon: Icon }) => {
                const active = to === '/' ? pathname === '/' : pathname.startsWith(to)
                return (
                  <SidebarMenuItem key={to}>
                    <SidebarMenuButton
                      isActive={active}
                      render={<Link to={to} onClick={() => setOpenMobile(false)} />}
                    >
                      <Icon />
                      <span>{label}</span>
                    </SidebarMenuButton>
                    {to === '/watchlist' && pending > 0 && (
                      <SidebarMenuBadge aria-label={`${pending} drafts awaiting review`} title="MCP drafts and candidates awaiting review">
                        {pending}
                      </SidebarMenuBadge>
                    )}
                  </SidebarMenuItem>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  )
}
