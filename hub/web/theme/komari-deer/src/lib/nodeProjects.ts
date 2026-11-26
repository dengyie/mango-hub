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

/**
 * Match a node against the dashboard search query, including deployed projects
 * in public_remark, tags, and group.
 */
export function matchesNodeSearch(
  node: SearchableNode,
  rawTerm: string,
  status: "online" | "offline" | "unknown",
  isRegionMatch: (region: string, term: string) => boolean
): boolean {
  const term = rawTerm.trim().toLowerCase();
  if (!term) return true;

  const statusMatch =
    ((term === "online" || term === "在线") && status === "online") ||
    ((term === "offline" || term === "离线") && status === "offline");

  return (
    node.name.toLowerCase().includes(term) ||
    node.os.toLowerCase().includes(term) ||
    node.arch.toLowerCase().includes(term) ||
    (node.group ? node.group.toLowerCase().includes(term) : false) ||
    (node.tags ? node.tags.toLowerCase().includes(term) : false) ||
    (node.public_remark ? node.public_remark.toLowerCase().includes(term) : false) ||
    isRegionMatch(node.region, term) ||
    (!Number.isNaN(Number(term)) && node.price.toString().includes(term)) ||
    statusMatch
  );
}
