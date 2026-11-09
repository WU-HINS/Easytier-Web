import { request, type Machine, type NetworkSummary, type Summary } from '../../lib/http';

/**
 * The overview's own traffic.
 *
 * Endpoints live with the feature that owns them; the transport in lib/http only
 * knows how to send a request and how to fail. Other features that need the device or
 * network lists import them from devices/api.ts or networks/api.ts rather than
 * re-declaring the same URL (HiDNS constraint: share by explicit export).
 */
export const dashboardApi = {
  summary: () => request<Summary>('/summary'),
  machines: () => request<{ machines: Machine[] }>('/machines').then((r) => r.machines ?? []),
  networks: () => request<{ networks: NetworkSummary[] }>('/networks').then((r) => r.networks ?? []),
};
