import { request } from '../../lib/http';

/** Account self-service: the password and the OIDC binding. */
export const accountApi = {
  oidcBinding: () => request<{ bound: boolean; issuer: string }>('/account/oidc'),
  unbindOidc: () => request<void>('/account/oidc/unbind', { method: 'POST' }),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>('/account/password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    }),
};
