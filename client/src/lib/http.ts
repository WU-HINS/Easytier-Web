/**
 * Typed HTTP layer. Every request goes through request.ts so cookie handling,
 * error mapping and the API prefix live in exactly one place.
 */
import { request } from "./request";

export { ApiError, API_PREFIX, request } from "./request";
export interface ConsoleInfo {
  username: string;
  role: string;
  config_server_protocol: string;
  config_server_port: number;
  webhook_auth: boolean;
}

export interface Summary {
  device_count: number;
  online_device_count: number;
}

export interface Machine {
  machine_id: string;
  hostname: string;
  alias: string;
  easytier_version: string;
  client_url: string;
  last_seen: string;
  online: boolean;
  running_network_instances: string[];
}

export interface NetworkSummary {
  network_id: string;
  display_name: string;
  network_name: string;
  networking_method: string;
  member_count: number;
  secure_mode: boolean;
}

export interface BlockedMachine {
  machine_id: string;
  hostname: string;
  blocked_time: string;
  attempt_count: number;
  last_attempt_time?: string;
}

export interface NetworkDetail {
  network_id: string;
  display_name: string;
  network_name: string;
  networking_method: string;
  peer_urls: string[] | null;
  virtual_cidr: string | null;
  secure_mode: Record<string, unknown> | null;
}

export interface AclStatsResponse {
  rules?: AclRuleStat[];
  global?: Record<string, number>;
}

export interface AclRuleStat {
  rule?: { id?: string; name?: string };
  stat?: { packet_count?: number; byte_count?: number };
}

export interface NetworkMember {
  member_id: string;
  device_id: string;
  hostname?: string;
  virtual_ipv4?: string;
  instance_id?: string;
  online: boolean;
  temporary: boolean;
  /** The device still runs this instance. */
  running?: boolean;
  /** The stored revision differs from the compiled one: a push is due. */
  pending?: boolean;
}

export interface NetworkMembers {
  members: NetworkMember[];
  compile_error?: string;
}

export interface RouteEntry {
  peer_id?: number;
  hostname?: string;
  cost?: number;
  path_latency?: number;
  next_hop_peer_id?: number;
  ipv4_addr?: { address?: { addr?: number }; network_length?: number };
}

export interface NetworkCredential {
  credential_id: string;
  expiry_unix: number;
  acl_groups: string[] | null;
  allow_relay: boolean;
  allowed_proxy_cidrs: string[] | null;
  reusable: boolean;
}

export interface RpcCapability {
  name: string;
  available: boolean;
  reason?: string;
}

export interface RpcCapabilities {
  services: { name: string; methods: RpcCapability[] }[];
}

export interface OidcConfig {
  enabled: boolean;
  button_label?: string;
  login_path?: string;
}

export interface AdminUser {
  id: number;
  username: string;
  role: string;
  disabled: boolean;
  oidc_bound: boolean;
  created_at: string;
  device_count: number;
  online_instances: number;
}

export interface AdminSettings {
  max_instances_per_user: number;
  password_min_length: number;
  password_min_classes: number;
  reserved_usernames: string;
  background_url: string;
  registration_open: boolean;
  oidc_enabled: boolean;
  oidc_issuer: string;
  oidc_client_id: string;
  oidc_client_secret_set: boolean;
  oidc_button_label: string;
  extra_tcp_enabled: boolean;
  extra_udp_enabled: boolean;
  extra_port: number;
  revision: number;
}

export interface SetupStatus {
  initialized: boolean;
}

export const api = {
  login: (username: string, password: string) =>
    request<{ username: string; display_name?: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ status: string; redirect?: string }>('/auth/logout', { method: 'POST' }),
  oidcConfig: () => request<OidcConfig>('/auth/oidc/config'),
  setupStatus: () => request<SetupStatus>('/setup'),
  setup: (username: string, password: string) =>
    request<{ username: string; role: string }>('/setup', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  register: (username: string, password: string) =>
    request<{ username: string; role: string }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  adminUsers: () => request<{ users: AdminUser[] }>('/admin/users').then((r) => r.users ?? []),
  setUserDisabled: (id: number, disabled: boolean) =>
    request<void>(`/admin/users/${id}/disabled`, { method: 'POST', body: JSON.stringify({ disabled }) }),
  deleteUser: (id: number) => request<void>(`/admin/users/${id}`, { method: 'DELETE' }),
  adminSettings: () => request<AdminSettings>('/admin/settings'),
  saveAdminSettings: (patch: Record<string, unknown>) =>
    request<AdminSettings>('/admin/settings', { method: 'PUT', body: JSON.stringify(patch) }),
  accountOidc: () => request<{ bound: boolean; issuer: string }>('/account/oidc'),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<void>("/account/password", {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    }),
  unbindOidc: () => request<void>('/account/oidc/unbind', { method: 'POST' }),
  consoleInfo: () => request<ConsoleInfo>('/console-info'),
  summary: () => request<Summary>('/summary'),
  renameMachine: (machineID: string, alias: string) =>
    request<void>('/machines/' + machineID + '/alias', {
      method: 'PUT',
      body: JSON.stringify({ alias }),
    }),
  deleteMachine: (machineID: string) => request<void>('/machines/' + machineID, { method: 'DELETE' }),
  rpcCapabilities: () => request<RpcCapabilities>('/rpc-capabilities'),
  machineRoutes: (machineID: string, instanceID: string) => {
    const parts = uuidParts(instanceID);
    return request<{ routes: RouteEntry[] }>('/machines/' + machineID + '/proxy-rpc', {
      method: 'POST',
      body: JSON.stringify({
        service_name: 'api.instance.PeerManageRpc',
        method_name: 'list_route',
        payload: { instance: { id: parts } },
      }),
    });
  },
  blockedMachines: () => request<{ blocked: BlockedMachine[] }>('/machines/blocked').then((r) => r.blocked ?? []),
  unblockMachine: (machineID: string) => request<void>('/machines/blocked/' + machineID, { method: 'DELETE' }),
  machines: () => request<{ machines: Machine[] }>('/machines').then((r) => r.machines ?? []),
  networks: () => request<{ networks: NetworkSummary[] }>('/networks').then((r) => r.networks ?? []),
  updateNetwork: (networkID: string, patch: Record<string, unknown>) =>
    request<{ network_id: string; display_name: string }>('/networks/' + networkID, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  aclPolicy: (networkID: string) =>
    request<{ acl_policy: string }>('/networks/' + networkID + '/acl-policy'),
  putAclPolicy: (networkID: string, policy: string) =>
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
  // ACL counters live on the device, one call per running instance.
  aclStats: (machineID: string, instanceID: string) =>
    request<AclStatsResponse>(
      `/machines/${machineID}/networks/info/${instanceID}/acl-stats`,
    ),
  networkMembers: (networkID: string) =>
    request<NetworkMembers>('/networks/' + networkID + '/members'),
  network: (networkID: string) => request<NetworkDetail>('/networks/' + networkID),
  memberConfig: (networkID: string, deviceID: string) =>
    request<{ override_config: string }>('/networks/' + networkID + '/members/' + deviceID + '/config')
      .then((r) => r.override_config ?? ''),
  putMemberConfig: (networkID: string, deviceID: string, override: string) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID + '/config', {
      method: 'PUT',
      body: JSON.stringify({ override_config: override }),
    }),
  // Field scoped on purpose: the server only writes the columns named here, so
  // two people editing different fields cannot undo each other (F-228).
  patchMember: (networkID: string, deviceID: string, patch: { hostname_override?: string; virtual_ipv4?: string }) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  addNetworkMembers: (networkID: string, deviceIDs: string[]) =>
    request<void>('/networks/' + networkID + '/members', {
      method: 'POST',
      body: JSON.stringify({ device_ids: deviceIDs }),
    }),
  // First OIDC sign-in: the console carries the verified identity in a signed
  // cookie, and the name chosen here becomes the account name for good.
  // Network level settings: one JSON blob shared by the compiler layers.
  networkAdvanced: (networkID: string) =>
    request<{ advanced: string }>('/networks/' + networkID + '/advanced'),
  putNetworkAdvanced: (networkID: string, advanced: string) =>
    request<void>('/networks/' + networkID + '/advanced', {
      method: 'PUT',
      body: JSON.stringify({ advanced }),
    }),
  registerOidc: (username: string) =>
    request<{ username: string }>('/auth/oidc/register', {
      method: 'POST',
      body: JSON.stringify({ username }),
    }),
  removeNetworkMember: (networkID: string, deviceID: string) =>
    request<void>('/networks/' + networkID + '/members/' + deviceID, { method: 'DELETE' }),
  deleteNetwork: (networkID: string) => request<void>('/networks/' + networkID, { method: 'DELETE' }),
  // The wizard decides the method and its extra fields, so the call takes the
  // whole settings object instead of a fixed pair of arguments.
  createNetwork: (settings: {
    display_name: string;
    networking_method: string;
    virtual_cidr: string;
    peer_urls?: string[];
    public_server_url?: string;
    secure_mode?: boolean;
  }) =>
    request<{ network_id: string }>('/networks', {
      method: "POST",
      body: JSON.stringify({ settings }),
    }),
};

import { uuidParts } from './wire';

export { uuidParts, formatIpv4 } from "./wire";
export { absolute, oidcCallbackURL, isSecureOrigin } from "./origin";
