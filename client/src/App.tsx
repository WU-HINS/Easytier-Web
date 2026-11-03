import { Navigate, Route, Routes } from 'react-router-dom';
import { ProtectedShell } from './app/protected-shell';
import { SetupGate } from './app/setup-gate';
import { AppShell } from './app/app-shell';
import { LoginPage } from './features/auth/login-page';
import { DashboardPage } from './features/dashboard/dashboard-page';
import { DevicesPage } from './features/devices/devices-page';
import { NetworksPage } from './features/networks/networks-page';
import { AppearancePage } from './features/settings/appearance-page';
import { AccountPage } from './features/account/account-page';
import { AdminUsersPage } from './features/admin/users-page';
import { AdminSettingsPage } from './features/admin/settings-page';
import { SetupPage } from './features/setup/setup-page';

export default function App() {
  return (
    <Routes>
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/login" element={<SetupGate><LoginPage /></SetupGate>} />
      <Route element={<SetupGate><ProtectedShell /></SetupGate>}>
        <Route element={<AppShell />}>
          <Route index element={<DashboardPage />} />
          <Route path="/devices" element={<DevicesPage />} />
          <Route path="/networks" element={<NetworksPage />} />
          <Route path="/account" element={<AccountPage />} />
          <Route path="/settings" element={<AppearancePage />} />
          <Route path="/admin/users" element={<AdminUsersPage />} />
          <Route path="/admin/settings" element={<AdminSettingsPage />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
