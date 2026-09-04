export const NW = 216, NH = 78;
export const GROUP_INSET = 18, GROUP_HEADER = 58;
const HGAP = 26, VGAP = 36, GROUP_GAP = 60, WRAP = 3;

export interface Position { x: number; y: number }

export interface GroupBounds extends Position {
  w: number;
  h: number;
  count: number;
}

export function fitLabel(text: string, maxChars: number): string {
  if (text.length <= maxChars) return text;
  return text.slice(0, Math.max(1, maxChars - 1)) + "…";
}

// subnetKey groups hosts into /24 rails. Strict IPv4: dotted quads of plain
// decimal octets 0-255 only - anything else (hex, exponents, spaces,
// out-of-range) belongs on the "other" rail rather than a bogus group.
const OCTET = /^\d{1,3}$/;

export function subnetKey(addr: string): string {
  const parts = addr.split(".");
  if (ipv4ToInt(addr) === undefined) return "other";
  return `${parts[0]}.${parts[1]}.${parts[2]}`;
}

// ipv4ToInt parses one strict dotted quad into a 32-bit number; undefined
// for anything else, with the same strictness subnetKey applies to hosts.
function ipv4ToInt(addr: string): number | undefined {
  const parts = addr.split(".");
  if (parts.length !== 4) return undefined;
  let value = 0;
  for (const p of parts) {
    if (!OCTET.test(p)) return undefined;
    const octet = Number(p);
    if (octet > 255) return undefined;
    value = value * 256 + octet;
  }
  return value;
}

// cidrCovers reports whether a "a.b.c.d/prefix" route contains a host
// address: real CIDR membership, so a /8 covers every /24 group inside it
// and not just the one sharing its first three octets. Anything
// unparseable - a missing prefix, a non-IPv4 base, a non-IPv4 host -
// covers nothing: a route we cannot parse must never sweep the shared
// "other" rail into ROUTED. A route without a prefix counts as /32.
export function cidrCovers(route: string, addr: string): boolean {
  const slash = route.lastIndexOf("/");
  const prefixText = slash < 0 ? "32" : route.slice(slash + 1);
  if (prefixText === "") return false;
  const bits = Number(prefixText);
  if (!Number.isInteger(bits) || bits < 0 || bits > 32) return false;
  const net = ipv4ToInt(slash < 0 ? route : route.slice(0, slash));
  const ip = ipv4ToInt(addr);
  if (net === undefined || ip === undefined) return false;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return ((net & mask) >>> 0) === ((ip & mask) >>> 0);
}

// geometrySignature is everything the topology's auto-fit reacts to: the
// host set, the route set, and which host each route's owning session
// lives on (that is where the edge starts). Resource churn that cannot
// move a node - jobs, creds, operators, module ranks - leaves the
// operator's pan and zoom alone.
export function geometrySignature(
  hosts: readonly ({ address: string } | null | undefined)[],
  routes: readonly ({ subnet: string; sessionId: string } | null | undefined)[],
  sessions: Readonly<Record<string, { targetHost?: string; sessionHost?: string } | null | undefined>>,
): string {
  const hostPart = hosts.filter(h => !!h).map(h => h!.address).join("|");
  const routePart = routes.filter(r => !!r).map(r => {
    const s = sessions[r!.sessionId];
    const source = s ? (s.targetHost || s.sessionHost || "") : "";
    return `${r!.subnet}>${r!.sessionId}@${source}`;
  }).join("|");
  return `${hostPart}#${routePart}`;
}

function groupedAddresses(hosts: { address: string }[]): Map<string, string[]> {
  const groups = new Map<string, string[]>();
  for (const h of hosts) {
    const key = subnetKey(h.address);
    const addresses = groups.get(key) ?? [];
    addresses.push(h.address);
    groups.set(key, addresses);
  }

  return new Map([...groups].sort(([a], [b]) => {
    if (a === "other") return 1;
    if (b === "other") return -1;
    return a.localeCompare(b, undefined, { numeric: true });
  }).map(([key, addresses]) => [key, addresses.sort((a, b) => {
    const aLast = Number(a.split(".")[3] ?? 0);
    const bLast = Number(b.split(".")[3] ?? 0);
    return aLast - bLast;
  })]));
}

export function baseLayout(hosts: { address: string }[]): Map<string, Position> {
  const positions = new Map<string, Position>();
  let groupY = 0;

  for (const addresses of groupedAddresses(hosts).values()) {
    addresses.forEach((address, index) => {
      const column = index % WRAP;
      const row = Math.floor(index / WRAP);
      positions.set(address, {
        x: GROUP_INSET + column * (NW + HGAP),
        y: groupY + GROUP_HEADER + row * (NH + VGAP),
      });
    });

    const rows = Math.ceil(addresses.length / WRAP);
    groupY += GROUP_HEADER + rows * NH + Math.max(0, rows - 1) * VGAP + GROUP_INSET + GROUP_GAP;
  }

  return positions;
}

export function layoutHosts(
  hosts: { address: string }[],
  sticky: Map<string, Position>,
): Map<string, Position> {
  const positions = baseLayout(hosts);
  for (const [address, position] of sticky) {
    if (positions.has(address)) positions.set(address, position);
  }
  return positions;
}

export function subnetGroups(
  hosts: { address: string }[],
  positions: Map<string, Position> = baseLayout(hosts),
): Map<string, GroupBounds> {
  const groups = new Map<string, GroupBounds>();

  for (const [key, addresses] of groupedAddresses(hosts)) {
    const points = addresses.map(address => positions.get(address)).filter((p): p is Position => !!p);
    if (points.length === 0) continue;
    const minX = Math.min(...points.map(p => p.x));
    const minY = Math.min(...points.map(p => p.y));
    const maxX = Math.max(...points.map(p => p.x + NW));
    const maxY = Math.max(...points.map(p => p.y + NH));
    groups.set(key, {
      x: minX - GROUP_INSET,
      y: minY - GROUP_HEADER,
      w: maxX - minX + GROUP_INSET * 2,
      h: maxY - minY + GROUP_HEADER + GROUP_INSET,
      count: addresses.length,
    });
  }

  return groups;
}
