import type { NodeBasicInfo } from "@/contexts/NodeListContext";

/**
 * Parse deployed project names from a node's public_remark field.
 * Supports comma, Chinese comma, enumeration comma, semicolon, middle dot, or newline separators.
 */
export function parseNodeProjects(publicRemark?: string | null): string[] {
  if (!publicRemark) return [];
  const raw = publicRemark.trim();
  if (!raw) return [];

  const seen = new Set<string>();
  const result: string[] = [];
  for (const part of raw.split(/[,，、;；·\r\n]+/)) {
    const item = part.trim();
    if (!item || seen.has(item)) continue;
    seen.add(item);
    result.push(item);
  }
  return result;
}

/**
 * Map a raw RPC2 client record into a typed NodeBasicInfo object.
 * Centralized here so public_remark and other fields are never dropped at the boundary.
 */
export function mapClientRecord(
  n: Record<string, any>,
  normalizeTrafficLimit: (value: unknown) => number
): NodeBasicInfo {
  return {
    uuid: n.uuid,
    name: n.name,
    cpu_name: n.cpu_name,
    virtualization: n.virtualization,
    arch: n.arch,
    cpu_cores: n.cpu_cores,
    os: n.os,
    kernel_version: n.kernel_version,
    gpu_name: n.gpu_name,
    region: n.region,
    mem_total: n.mem_total,
    swap_total: n.swap_total,
    disk_total: n.disk_total,
    version: n.version ?? "",
    weight: n.weight ?? 0,
    price: n.price ?? 0,
    tags: n.tags ?? "",
    billing_cycle: n.billing_cycle ?? 0,
    currency: n.currency ?? "",
    group: n.group ?? "",
    public_remark: typeof n.public_remark === "string" ? n.public_remark : "",
    traffic_limit: normalizeTrafficLimit(n.traffic_limit),
    traffic_limit_type: n.traffic_limit_type,
    expired_at: n.expired_at ?? "",
    created_at: n.created_at ?? "",
    updated_at: n.updated_at ?? "",
    ipv4: n.ipv4,
    ipv6: n.ipv6,
  };
}

export interface SearchableNode {
  name: string;
  os: string;
  arch: string;
  region: string;
  price: number;
  group?: string;
  tags?: string;
  public_remark?: string;
}

function matchesSingleToken(
  node: SearchableNode,
  token: string,
  status: "online" | "offline" | "unknown",
  isRegionMatch: (region: string, term: string) => boolean
): boolean {
  const statusMatch =
    ((token === "online" || token === "在线") && status === "online") ||
    ((token === "offline" || token === "离线") && status === "offline");

  return (
    node.name.toLowerCase().includes(token) ||
    node.os.toLowerCase().includes(token) ||
    node.arch.toLowerCase().includes(token) ||
    (node.group ? node.group.toLowerCase().includes(token) : false) ||
    (node.tags ? node.tags.toLowerCase().includes(token) : false) ||
    (node.public_remark ? node.public_remark.toLowerCase().includes(token) : false) ||
    isRegionMatch(node.region, token) ||
    (!Number.isNaN(Number(token)) && node.price.toString().includes(token)) ||
    statusMatch
  );
}

/**
 * Match a node against the dashboard search query (supports space-separated AND tokens),
 * including deployed projects in public_remark, tags, and group.
 */
export function matchesNodeSearch(
  node: SearchableNode,
  rawTerm: string,
  status: "online" | "offline" | "unknown",
  isRegionMatch: (region: string, term: string) => boolean
): boolean {
  const tokens = rawTerm.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return true;
  return tokens.every((token) => matchesSingleToken(node, token, status, isRegionMatch));
}
