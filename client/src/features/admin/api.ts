import { request, type AdminSettings, type AdminUser } from '../../lib/http';

/**
 * Administrator-only traffic: the account list and the console's own settings.
 *
 * Both live behind require-admin on the server, so a caller that reaches these
 * endpoints has already been authorised; the pages still hide their controls when the
 * role check fails (see app/require-admin.tsx).
 */
export const adminApi = {
  users: () => request<{ users: AdminUser[] }>('/admin/users').then((r) => r.users ?? []),
  setUserDisabled: (id: number, disabled: boolean) =>
    request<void>('/admin/users/' + id + '/disabled', { method: 'POST', body: JSON.stringify({ disabled }) }),
  deleteUser: (id: number) => request<void>('/admin/users/' + id, { method: 'DELETE' }),
  settings: () => request<AdminSettings>('/admin/settings'),
  saveSettings: (patch: Record<string, unknown>) =>
    request<AdminSettings>('/admin/settings', { method: 'PUT', body: JSON.stringify(patch) }),
};
