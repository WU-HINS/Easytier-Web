import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban, KeyRound, LayoutDashboard, LogOut, Monitor, Network, Server, Settings, Sun, Moon, UserCog, Users } from 'lucide-react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useCopy, useLocale } from '../copy/locale';
import { api } from '../lib/http';
import { useTheme } from '../lib/theme';
import { Button, cn } from '../ui';

const buildBaseNavigation = (copy: ReturnType<typeof useCopy>) => [
  { to: '/', label: copy.nav.dashboard, icon: LayoutDashboard, end: true },
  { to: '/devices', label: copy.nav.devices, icon: Server, end: false },
  { to: '/networks', label: copy.nav.networks, icon: Network, end: false },
  { to: '/devices/blocked', label: copy.nav.blocked, icon: Ban, end: false },
  { to: '/account/password', label: copy.nav.password, icon: KeyRound, end: false },
  { to: '/account', label: copy.nav.account, icon: UserCog, end: false },
  { to: '/settings', label: copy.nav.settings, icon: Monitor, end: false },
];

const buildAdminNavigation = (copy: ReturnType<typeof useCopy>) => [
  { to: '/admin/users', label: copy.nav.adminUsers, icon: Users, end: false },
  { to: '/admin/settings', label: copy.nav.adminSettings, icon: Settings, end: false },
];

const themeIcon = { light: Sun, dark: Moon, auto: Monitor } as const;

export function AppShell() {
  const { mode, setMode } = useTheme();
  const info = useQuery({ queryKey: ['console-info'], queryFn: api.consoleInfo });
  const copy = useCopy();
  const { locale, setLocale } = useLocale();
  const navigation =
    info.data && info.data.role === "admin"
      ? [...buildBaseNavigation(copy), ...buildAdminNavigation(copy)]
      : buildBaseNavigation(copy);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const Icon = themeIcon[mode];

  const logout = useMutation({
    mutationFn: api.logout,
    onSuccess: (result) => {
      queryClient.clear();
      if (result?.redirect) {
        window.location.href = result.redirect;
        return;
      }
      navigate('/login', { replace: true });
    },
  });

  const cycle = () => setMode(mode === 'auto' ? 'light' : mode === 'light' ? 'dark' : 'auto');

  return (
    <div className="min-h-screen lg:grid" style={{ gridTemplateColumns: 'var(--w-sidebar) 1fr' }}>
      <aside className="chrome hidden border-r border-border px-[var(--u-6)] py-[var(--u-7)] lg:block">
        <div className="flex items-baseline gap-[var(--u-2)]">
          <span className="text-phi-lg font-semibold tracking-tight">{copy.brand}</span>
          <span className="text-phi-sm text-muted-foreground">{copy.brandTag}</span>
        </div>
        {/* WCAG 2.4.1: the sidebar owns the first ten tab stops on every page, so
            keyboard users get a way to jump past it. */}
        <a
          href="#main-content"
          className="sr-only focus:not-sr-only focus:absolute focus:left-[var(--gap-card)] focus:top-[var(--u-4)] focus:z-50 focus:rounded-[var(--radius)] focus:bg-card focus:px-[var(--u-4)] focus:py-[var(--u-2)] focus:text-phi-sm focus:text-foreground"
        >
          {copy.shell.skipToContent}
        </a>
        <nav className="mt-[var(--gap-section)] flex flex-col gap-[var(--u-1)]">
          {navigation.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-[var(--u-3)] rounded-[var(--radius)] px-[var(--u-4)] py-[var(--u-3)] text-phi-base transition-colors',
                  isActive ? 'bg-primary/12 text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )
              }
            >
              <item.icon size={16} aria-hidden />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-col">
        <header className="chrome sticky top-0 z-10 flex h-[var(--h-header)] items-center justify-between border-b border-border px-[var(--gap-card)]">
          <div className="flex items-center gap-[var(--u-3)] lg:hidden">
            <span className="text-phi-base font-semibold">{copy.brand}</span>
          </div>
          <div className="hidden text-phi-sm text-muted-foreground lg:block">{copy.brandTag}</div>
          <div className="flex items-center gap-[var(--u-2)]">
            <select
              aria-label={copy.shell.language}
              value={locale}
              onChange={(event) => setLocale(event.target.value === "en-US" ? "en-US" : "zh-CN")}
              className="rounded-[var(--radius)] border border-input bg-card p-[var(--u-1)] text-phi-sm"
            >
              <option value="zh-CN">{copy.shell.languageZh}</option>
              <option value="en-US">{copy.shell.languageEn}</option>
            </select>
            <a
              className="inline-flex items-center min-h-[var(--u-11)] text-phi-sm text-muted-foreground underline underline-offset-4"
              href="https://easytier.cn/"
              target="_blank"
              rel="noreferrer"
            >
              {copy.nav.docs}
            </a>
            <Button variant="ghost" size="sm" onClick={cycle} aria-label={copy.common.theme}>
              <Icon size={16} aria-hidden />
            </Button>
            <Button variant="ghost" size="sm" onClick={() => logout.mutate()} aria-label={copy.common.logout}>
              <LogOut size={16} aria-hidden />
            </Button>
          </div>
        </header>
        <main id="main-content" tabIndex={-1} className="mx-auto w-full max-w-[var(--w-content)] flex-1 px-[var(--gap-card)] py-[var(--gap-section)]">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
