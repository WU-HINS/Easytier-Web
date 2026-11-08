import { Ban, KeyRound, LayoutDashboard, Monitor, Network, Server, Settings, UserCog, Users, type LucideIcon } from 'lucide-react';
import type { Copy } from '../copy/locale';

/**
 * Navigation is declared here and nowhere else.
 *
 * The sidebar used to be assembled inline in the shell, which is how nine entries
 * ended up in one flat list with the operator's pages next to their own account
 * settings (review round r259, R-B). Grouping is part of the information
 * architecture, so it lives with the routes rather than inside the markup.
 */
export type NavItem = { to: string; label: string; icon: LucideIcon; end?: boolean };
export type NavGroup = { key: 'workspace' | 'account' | 'admin'; label?: string; items: NavItem[] };

export function buildNavigation(copy: Copy, role: string | undefined): NavGroup[] {
  const groups: NavGroup[] = [
    {
      key: 'workspace',
      items: [
        { to: '/', label: copy.nav.dashboard, icon: LayoutDashboard, end: true },
        { to: '/devices', label: copy.nav.devices, icon: Server },
        { to: '/networks', label: copy.nav.networks, icon: Network },
        { to: '/devices/blocked', label: copy.nav.blocked, icon: Ban },
      ],
    },
    {
      key: 'account',
      label: copy.nav.groupAccount,
      items: [
        { to: '/account', label: copy.nav.account, icon: UserCog },
        { to: '/account/password', label: copy.nav.password, icon: KeyRound },
        { to: '/settings', label: copy.nav.settings, icon: Monitor },
      ],
    },
  ];
  if (role === 'admin') {
    groups.push({
      key: 'admin',
      label: copy.nav.groupAdmin,
      items: [
        { to: '/admin/users', label: copy.nav.adminUsers, icon: Users },
        { to: '/admin/settings', label: copy.nav.adminSettings, icon: Settings },
      ],
    });
  }
  return groups;
}
