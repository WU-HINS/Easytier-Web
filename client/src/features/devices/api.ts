import {
  request,
  uuidParts,
  type BlockedMachine,
  type Machine,
  type RouteEntry,
  type RpcCapabilities,
} from '../../lib/http';

/** Everything the devices surfaces ask the console for. */
export const devicesApi = {
  machines: () => request<{ machines: Machine[] }>('/machines').then((r) => r.machines ?? []),
  rename: (machineID: string, alias: string) =>
    request<void>('/machines/' + machineID + '/alias', { method: 'PUT', body: JSON.stringify({ alias }) }),
  remove: (machineID: string) => request<void>('/machines/' + machineID, { method: 'DELETE' }),
  blocked: () => request<{ blocked: BlockedMachine[] }>('/machines/blocked').then((r) => r.blocked ?? []),
  unblock: (machineID: string) => request<void>('/machines/blocked/' + machineID, { method: 'DELETE' }),
  capabilities: () => request<RpcCapabilities>('/rpc-capabilities'),
  routes: (machineID: string, instanceID: string) =>
    request<{ routes: RouteEntry[] }>('/machines/' + machineID + '/proxy-rpc', {
      method: 'POST',
      body: JSON.stringify({
        service_name: 'api.instance.PeerManageRpc',
        method_name: 'list_route',
        payload: { instance: { id: uuidParts(instanceID) } },
      }),
    }),
};
