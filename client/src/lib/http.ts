/**
 * Typed HTTP layer. Every request goes through request.ts so cookie handling,
 * error mapping and the API prefix live in exactly one place.
 */

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
  // Server decides, the page hides controls: a read-only network must not offer
  // buttons that answer 403 (HiDNS convention).
  can_write?: boolean;
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
  can_write?: boolean;
}

export interface AclStatsResponse {
  rules?: AclRuleStat[];
  global?: Record<string, number>;
}

export interface InstancePeerConn {
  tunnel?: { tunnelType?: string; remoteAddr?: { host?: string } };
  stats?: { rxBytes?: number; txBytes?: number; latencyUs?: number };
  lossRate?: number;
  isClosed?: boolean;
}

export interface InstancePeer {
  peerId?: number;
  conns?: InstancePeerConn[];
}

export interface InstanceRoute {
  peerId?: number;
  hostname?: string;
  cost?: number;
  pathLatency?: number;
  nextHopPeerId?: number;
}

export interface InstanceRunningInfo {
  running?: boolean;
  errorMsg?: string;
  peers?: InstancePeer[];
  routes?: InstanceRoute[];
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
  registration_open?: boolean;
}

// Every endpoint now lives with the feature that owns it (features/<domain>/api.ts).
// This module is the transport: request(), ApiError, the base URL and the shared types.


export { uuidParts, formatIpv4 } from "./wire";
export { absolute, oidcCallbackURL, isSecureOrigin } from "./origin";
