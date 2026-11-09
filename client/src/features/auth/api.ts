import { request, type ConsoleInfo, type OidcConfig, type SetupStatus } from '../../lib/http';

/**
 * Sign-in, sign-up and the identity facts the shell asks for.
 *
 * console-info lives here because auth owns "who am I": the shell, the route guards
 * and the enrolment card all read it from this module rather than each declaring the
 * URL again (HiDNS constraint: share by explicit export).
 */
export const authApi = {
  login: (username: string, password: string) =>
    request<{ username: string; display_name?: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ status: string; redirect?: string }>('/auth/logout', { method: 'POST' }),
  register: (username: string, password: string) =>
    request<{ username: string; role: string }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  registerOidc: (username: string) =>
    request<{ username: string }>('/auth/oidc/register', {
      method: 'POST',
      body: JSON.stringify({ username }),
    }),
  consoleInfo: () => request<ConsoleInfo>('/console-info'),
  oidcConfig: () => request<OidcConfig>('/auth/oidc/config'),
  setupStatus: () => request<SetupStatus>('/setup'),
};
