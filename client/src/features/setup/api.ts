import { request } from '../../lib/http';

/** First-run setup. One call, one administrator, and the endpoint closes for good. */
export const setupApi = {
  createAdmin: (username: string, password: string) =>
    request<{ username: string; role: string }>('/setup', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
};
