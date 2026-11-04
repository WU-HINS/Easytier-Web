/**
 * The ACL policy the server stores: a default action plus an ordered list of
 * rules. The visual editor and the JSON view both read and write this shape, so
 * the two can never drift.
 */
export interface AclSelector {
  type: string;
  member_id?: string;
  cidrs?: string[];
  name?: string;
}

export interface AclProtocol {
  protocol: string;
  ports?: string[];
  stateful?: boolean;
}

export interface AclRule {
  id: string;
  name: string;
  enabled: boolean;
  action: string;
  sources?: AclSelector[];
  destinations?: AclSelector[];
  protocols?: AclProtocol[];
}

export interface AclPolicy {
  default_action: string;
  rules: AclRule[];
}

export const emptyPolicy = (): AclPolicy => ({ default_action: 'allow', rules: [] });

/** parsePolicy is tolerant: an empty or unreadable blob means allow-all. */
export function parsePolicy(raw: string): AclPolicy {
  if (!raw.trim()) return emptyPolicy();
  try {
    const parsed = JSON.parse(raw) as Partial<AclPolicy>;
    const rules = Array.isArray(parsed.rules) ? parsed.rules : [];
    // Rules written before ids were random can share one. React keys and the
    // edit handlers both assume uniqueness, so duplicates get a suffix here.
    const seen = new Set<string>();
    const unique = rules.map((rule) => {
      let id = rule.id || "rule";
      let suffix = 2;
      while (seen.has(id)) {
        id = (rule.id || "rule") + "-" + suffix;
        suffix += 1;
      }
      seen.add(id);
      return rule.id === id ? rule : { ...rule, id };
    });
    return {
      default_action: parsed.default_action === "deny" ? "deny" : "allow",
      rules: unique,
    };
  } catch {
    return emptyPolicy();
  }
}

export function policyToJson(policy: AclPolicy): string {
  return JSON.stringify(policy, null, 2);
}

/** selectorsToText renders a selector list as "any" or a comma separated CIDR list. */
export function selectorsToText(selectors?: AclSelector[]): string {
  const list = selectors ?? [];
  if (list.length === 0) return 'any';
  if (list.some((entry) => entry.type === 'any')) return 'any';
  const cidrs = list.flatMap((entry) => entry.cidrs ?? []);
  return cidrs.join(', ');
}

export function textToSelectors(text: string): AclSelector[] {
  const trimmed = text.trim();
  if (!trimmed || trimmed.toLowerCase() === 'any') return [{ type: 'any' }];
  const cidrs = trimmed
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '');
  return [{ type: 'cidr', cidrs }];
}

/** protocolsToText renders protocols as "tcp:80,443; udp:53" or "any". */
export function protocolsToText(protocols?: AclProtocol[]): string {
  const list = protocols ?? [];
  if (list.length === 0) return 'any';
  return list
    .map((entry) => (entry.ports && entry.ports.length > 0 ? entry.protocol + ':' + entry.ports.join(',') : entry.protocol))
    .join('; ');
}

/** textToProtocols parses "tcp:80,443; udp:53". Anything unreadable is dropped. */
export function textToProtocols(text: string): AclProtocol[] {
  const trimmed = text.trim();
  if (!trimmed || trimmed.toLowerCase() === 'any') return [{ protocol: 'any' }];
  const out: AclProtocol[] = [];
  for (const chunk of trimmed.split(';')) {
    const part = chunk.trim();
    if (!part) continue;
    const colon = part.indexOf(':');
    if (colon === -1) {
      out.push({ protocol: part.toLowerCase() });
      continue;
    }
    const protocol = part.slice(0, colon).trim().toLowerCase();
    const ports = part
      .slice(colon + 1)
      .split(',')
      .map((entry) => entry.trim())
      .filter((entry) => entry !== '');
    out.push(ports.length > 0 ? { protocol, ports } : { protocol });
  }
  return out.length > 0 ? out : [{ protocol: 'any' }];
}

/**
 * newRule builds a fresh allow rule. The id is random on purpose: the server
 * treats it as the rule identity, and a counter restarts at one on every page
 * load, which produced two rules with the same id.
 */
export function newRule(): AclRule {
  return {
    id: 'rule-' + Math.random().toString(36).slice(2, 10),
    name: '规则',
    enabled: true,
    action: 'allow',
    sources: [{ type: 'any' }],
    destinations: [{ type: 'any' }],
    protocols: [{ protocol: 'any' }],
  };
}
