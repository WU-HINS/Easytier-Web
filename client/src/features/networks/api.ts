import {
  request,
  type AclStatsResponse,
  type InstanceRunningInfo,
  type NetworkCredential,
  type NetworkDetail,
  type NetworkMembers,
  type NetworkSummary,
} from '../../lib/http';

/**
 * Every call the networks surfaces make.
 *
 * The console owns the intent and the device is told what to run, so most of these
 * are small: one network, its members, its policy. The two that reach a device
 * (instanceInfo, aclStats, loggerLevel) are named after the device fact they read.
 */
export const networksApi = {
  list: () => request<{ networks: NetworkSummary[] }>('/networks').then((r) => r.networks ?? []),
  get: (networkID: string) => request<NetworkDetail>('/networks/' + networkID),
  create: (settings: {
    display_name: string;
    networking_method: string;
    virtual_cidr: string;
    peer_urls?: string[];
    public_server_url?: string;
    secure_mode?: boolean;
  }) => request<{ network_id: string }>('/networks', { method: 'POST', body: JSON.stringify({ settings }) }),
  update: (networkID: string, patch: Record<string, unknown>) =>
    request<{ network_id: string; display_name: string }>('/networks/' + networkID, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  remove: (networkID: string) => request<void>('/networks/' + networkID, { method: 'DELETE' }),
  rotateSecret: (networkID: string) =>
    request<{ network_secret: string; rotated_at: string }>('/networks/' + networkID + '/rotate-secret', {
      method: 'POST',
    }),
  advanced: (networkID: string) => request<{ advanced: string }>('/networks/' + networkID + '/advanced'),
  saveAdvanced: (networkID: string, advanced: string) =>
    request<void>('/networks/' + networkID + '/advanced', { method: 'PUT', body: JSON.stringify({ advanced }) }),
  aclPolicy: (networkID: string) => request<{ acl_policy: string }>('/networks/' + networkID + '/acl-policy'),
  saveAclPolicy: (networkID: string, policy: string) =>
    request<void>('/networks/' + networkID + '/acl-policy', {
      method: 'PUT',
      body: JSON.stringify({ acl_policy: policy }),
    }),
  credentials: (networkID: string) =>
    request<{ credentials: NetworkCredential[] }>('/networks/' + networkID + '/credentials'),
  createCredential: (networkID: string, payload: Record<string, unknown>) =>
    request<{ credential_id: string; credential_secret: string }>('/networks/' + networkID + '/credentials', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  revokeCredential: (networkID: string, credentialID: string) =>
    request<void>('/networks/' + networkID + '/credentials/' + credentialID, { method: 'DELETE' }),
  members: (networkID: string) => request<NetworkMembers>('/networks/' + networkID + '/members'),
  addMembers: (networkID: string, deviceIDs: string[]) =>
    request<void>('/networks/' + networkID + '/members', {
      method: 'POST',
      body: JSON.stringify({ device_ids: deviceIDs }),
    }),
  removeMember: (networkID: string, deviceID: string) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID, { method: 'DELETE' }),
  // Field scoped on purpose: the server writes only the named columns, so two people
  // editing different fields cannot undo each other (F-228).
  patchMember: (networkID: string, deviceID: string, patch: { hostname_override?: string; virtual_ipv4?: string }) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID, { method: 'PATCH', body: JSON.stringify(patch) }),
  memberConfig: (networkID: string, deviceID: string) =>
    request<{ override_config: string }>('/networks/' + networkID + '/members/' + deviceID + '/config').then(
      (r) => r.override_config ?? '',
    ),
  saveMemberConfig: (networkID: string, deviceID: string, override: string) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID + '/config', {
      method: 'PUT',
      body: JSON.stringify({ override_config: override }),
    }),
  memberToml: (networkID: string, deviceID: string) =>
    request<{ toml_config?: string; error?: string }>('/networks/' + networkID + '/members/' + deviceID + '/toml'),
  parseConfig: (toml: string) =>
    request<{ config?: unknown; error?: string }>('/parse-config', {
      method: 'POST',
      body: JSON.stringify({ toml_config: toml }),
    }),
  instanceInfo: (machineID: string, instanceID: string) =>
    request<{ info?: Record<string, InstanceRunningInfo> }>('/machines/' + machineID + '/networks/info/' + instanceID),
  aclStats: (machineID: string, instanceID: string) =>
    request<AclStatsResponse>('/machines/' + machineID + '/networks/info/' + instanceID + '/acl-stats'),
  // The log level belongs to the device process, not to one instance.
  loggerLevel: (machineID: string) => request<{ level: string; value: number }>('/machines/' + machineID + '/logger-level'),
  setLoggerLevel: (machineID: string, level: string) =>
    request<{ level: string; value: number }>('/machines/' + machineID + '/logger-level', {
      method: 'PUT',
      body: JSON.stringify({ level }),
    }),
};
