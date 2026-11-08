import { Suspense, lazy } from 'react';
import { Route, Routes } from 'react-router-dom';
import { ProtectedShell } from './app/protected-shell';
import { RequireAdmin } from './app/require-admin';
import { NotFoundPage } from './app/not-found';
import { SetupGate } from './app/setup-gate';
import { AppShell } from './app/app-shell';
import { LoginPage } from './features/auth/login-page';
import { DashboardPage } from './features/dashboard/dashboard-page';
import { SetupPage } from './features/setup/setup-page';
import { useCopy } from './copy/locale';

// Everything behind the shell is split out. On a slow link the first screen used to
// wait for the whole console - 369 KB, about seven seconds at 50 KB/s - before it
// could paint anything. The pages load as they are opened; the shell, the login page
// and the overview stay in the entry chunk so the first screen is unchanged.
const DevicesPage = lazy(() => import('./features/devices/devices-page').then((m) => ({ default: m.DevicesPage })));
const DeviceDetailPage = lazy(() => import('./features/devices/device-detail-page').then((m) => ({ default: m.DeviceDetailPage })));
const BlockedPage = lazy(() => import('./features/devices/blocked-page').then((m) => ({ default: m.BlockedPage })));
const PasswordPage = lazy(() => import('./features/account/password-page').then((m) => ({ default: m.PasswordPage })));
const NetworksPage = lazy(() => import('./features/networks/networks-page').then((m) => ({ default: m.NetworksPage })));
const NetworkDetailPage = lazy(() => import('./features/networks/network-detail-page').then((m) => ({ default: m.NetworkDetailPage })));
const AccountPage = lazy(() => import('./features/account/account-page').then((m) => ({ default: m.AccountPage })));
const AppearancePage = lazy(() => import('./features/settings/appearance-page').then((m) => ({ default: m.AppearancePage })));
const AdminUsersPage = lazy(() => import('./features/admin/users-page').then((m) => ({ default: m.AdminUsersPage })));
const AdminSettingsPage = lazy(() => import('./features/admin/settings-page').then((m) => ({ default: m.AdminSettingsPage })));

// A screen reader has to know the route is loading, so this is a live status region
// rather than a bare spinner.
function RouteFallback() {
  const copy = useCopy();
  return (
    <div role="status" aria-live="polite" className="flex items-center justify-center gap-[var(--u-2)] p-[var(--pad-card)] text-phi-sm text-muted-foreground">
      <span aria-hidden="true" className="inline-block h-[var(--u-4)] w-[var(--u-4)] animate-spin rounded-full border-2 border-border border-t-primary" />
      {copy.common.routeLoading}
    </div>
  );
}

export default function App() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <Routes>
        <Route path="/setup" element={<SetupPage />} />
        <Route path="/login" element={<SetupGate><LoginPage /></SetupGate>} />
        <Route element={<SetupGate><ProtectedShell /></SetupGate>}>
          <Route element={<AppShell />}>
            <Route index element={<DashboardPage />} />
            <Route path="/devices" element={<DevicesPage />} />
            <Route path="/devices/:machineID" element={<DeviceDetailPage />} />
            <Route path="/devices/blocked" element={<BlockedPage />} />
            <Route path="/account/password" element={<PasswordPage />} />
            <Route path="/networks" element={<NetworksPage />} />
            <Route path="/networks/:networkID" element={<NetworkDetailPage />} />
            <Route path="/account" element={<AccountPage />} />
            <Route path="/settings" element={<AppearancePage />} />
            {/* One guard for every administrator page, so a new page cannot be
                added without the check. */}
            <Route element={<RequireAdmin />}>
              <Route path="/admin/users" element={<AdminUsersPage />} />
              <Route path="/admin/settings" element={<AdminSettingsPage />} />
            </Route>
            {/* Inside the shell, so an unknown address still has navigation; the
                address bar keeps the path the user asked for (R-C, r259). */}
            <Route path="*" element={<NotFoundPage />} />
          </Route>
        </Route>

      </Routes>
    </Suspense>
  );
}
