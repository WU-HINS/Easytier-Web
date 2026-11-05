/**
 * Map a parsed network config onto the member override shape.
 *
 * The console renders the whole effective config as TOML, but an override only
 * accepts a subset of it (central.Overrides), in snake_case. Passing the parsed
 * config straight through is rejected with "unknown field", which is what the
 * first version of this panel did.
 */
export interface MemberOverrides {
  no_tun?: boolean;
  disable_ipv6?: boolean;
  multi_thread?: boolean;
  latency_first?: boolean;
  proxy_cidrs?: string[];
  listener_urls?: string[];
  disable_p2p?: boolean;
  mtu?: number;
  instance_recv_bps_limit?: number;
  enable_exit_node?: boolean;
  disable_encryption?: boolean;
}

type Parsed = Record<string, unknown>;

const bool = (value: unknown): boolean | undefined => (typeof value === 'boolean' ? value : undefined);
const num = (value: unknown): number | undefined => (typeof value === 'number' ? value : undefined);
const strings = (value: unknown): string[] | undefined =>
  Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : undefined;

export function toMemberOverrides(config: unknown): MemberOverrides {
  const source = (config ?? {}) as Parsed;
  const out: MemberOverrides = {};

  const noTun = bool(source.noTun);
  if (noTun !== undefined) out.no_tun = noTun;
  const disableIpv6 = bool(source.disableIpv6);
  if (disableIpv6 !== undefined) out.disable_ipv6 = disableIpv6;
  const multiThread = bool(source.multiThread);
  if (multiThread !== undefined) out.multi_thread = multiThread;
  const latencyFirst = bool(source.latencyFirst);
  if (latencyFirst !== undefined) out.latency_first = latencyFirst;
  const disableP2p = bool(source.disableP2p);
  if (disableP2p !== undefined) out.disable_p2p = disableP2p;
  const enableExitNode = bool(source.enableExitNode);
  if (enableExitNode !== undefined) out.enable_exit_node = enableExitNode;
  const disableEncryption = bool(source.disableEncryption);
  if (disableEncryption !== undefined) out.disable_encryption = disableEncryption;

  const mtu = num(source.mtu);
  if (mtu !== undefined) out.mtu = mtu;
  const rateLimit = num(source.instanceRecvBpsLimit);
  if (rateLimit !== undefined) out.instance_recv_bps_limit = rateLimit;

  const listeners = strings(source.listenerUrls);
  if (listeners !== undefined) out.listener_urls = listeners;

  // Proxy networks arrive as objects; the override wants bare CIDR strings.
  const proxy = source.proxyNetworks;
  if (Array.isArray(proxy)) {
    const cidrs = proxy
      .map((entry) => (entry && typeof entry === 'object' ? (entry as Parsed).cidr : undefined))
      .filter((entry): entry is string => typeof entry === 'string');
    if (cidrs.length > 0) out.proxy_cidrs = cidrs;
  }

  return out;
}
