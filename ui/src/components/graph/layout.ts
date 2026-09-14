export const NW = 240, NH = 124;
export const GROUP_INSET = 22, GROUP_HEADER = 66;
const HGAP = 26, VGAP = 36, GROUP_GAP = 40;

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

export function baseLayout(hosts: { address: string }[], columns = 3, groupColumns = 1): Map<string, Position> {
  const positions = new Map<string, Position>();
  const groups = [...groupedAddresses(hosts).values()];
  const heights = groups.map(addresses => groupHeight(addresses.length, columns));
  const widths = groups.map(addresses => groupWidth(addresses.length, columns));
  const stackOf = stackAssignment(heights, groupColumns);
  const stackWidth = new Array<number>(groupColumns).fill(0);
  groups.forEach((_, i) => {
    stackWidth[stackOf[i]!] = Math.max(stackWidth[stackOf[i]!]!, widths[i]!);
  });
  const stackX: number[] = [];
  let x = 0;
  for (let stack = 0; stack < groupColumns; stack++) {
    stackX.push(x);
    x += stackWidth[stack]! + GROUP_GAP;
  }
  const stackY = new Array<number>(groupColumns).fill(0);

  groups.forEach((addresses, i) => {
    const stack = stackOf[i]!;
    const base = stackY[stack]!;
    addresses.forEach((address, index) => {
      const column = index % columns;
      const row = Math.floor(index / columns);
      positions.set(address, {
        x: stackX[stack]! + GROUP_INSET + column * (NW + HGAP),
        y: base + GROUP_HEADER + row * (NH + VGAP),
      });
    });
    stackY[stack] = base + heights[i]! + GROUP_GAP;
  });

  return positions;
}

export function layoutHosts(
  hosts: { address: string }[],
  sticky: Map<string, Position>,
  columns = 3,
  groupColumns = 1,
): Map<string, Position> {
  const positions = baseLayout(hosts, columns, groupColumns);
  for (const [address, position] of sticky) {
    if (positions.has(address)) positions.set(address, position);
  }
  return positions;
}

// Preserve held positions and place new hosts in free cells in their subnet
// so discovery does not move or overlap existing hosts.
export function layoutWithHeld(
  hosts: { address: string }[],
  held: Map<string, Position>,
  columns: number,
  groupColumns: number,
): Map<string, Position> {
  const positions = new Map(held);
  const base = baseLayout(hosts, columns, groupColumns);

  const occupied = (x: number, y: number) => {
    for (const p of positions.values()) {
      if (x < p.x + NW && x + NW > p.x && y < p.y + NH && y + NH > p.y) return true;
    }
    return false;
  };

  // per-subnet grid anchor: the top-left-most held host
  const anchors = new Map<string, Position>();
  for (const [address, position] of held) {
    const key = subnetKey(address);
    const anchor = anchors.get(key);
    if (!anchor || position.y < anchor.y || (position.y === anchor.y && position.x < anchor.x)) {
      anchors.set(key, { x: position.x, y: position.y });
    }
  }

  for (const [address, slot] of base) {
    if (positions.has(address)) continue;
    const anchor = anchors.get(subnetKey(address));
    if (!anchor) {
      positions.set(address, slot); // a wholly new subnet has nothing to hit
      continue;
    }
    let placed = false;
    for (let row = 0; row < 512 && !placed; row++) {
      for (let column = 0; column < 32 && !placed; column++) {
        const x = anchor.x + column * (NW + HGAP);
        const y = anchor.y + row * (NH + VGAP);
        if (!occupied(x, y)) {
          positions.set(address, { x, y });
          placed = true;
        }
      }
    }
    if (!placed) positions.set(address, slot); // unreachable grid cap
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

// Host and group column counts determine the aspect ratio used when fitting.
export interface LayoutPlan {
  columns: number;
  groupColumns: number;
}

const MAX_HOST_COLUMNS = 8;

function groupHeight(count: number, columns: number): number {
  const rows = Math.ceil(count / columns);
  return GROUP_HEADER + rows * NH + Math.max(0, rows - 1) * VGAP + GROUP_INSET;
}

function groupWidth(count: number, columns: number): number {
  const used = Math.min(count, columns);
  return used * NW + (used - 1) * HGAP + GROUP_INSET * 2;
}

// Subnet order makes the height-based assignment deterministic.
function stackAssignment(heights: number[], stacks: number): number[] {
  const totals = new Array<number>(stacks).fill(0);
  return heights.map(height => {
    let target = 0;
    for (let stack = 1; stack < stacks; stack++) if (totals[stack]! < totals[target]!) target = stack;
    totals[target] = totals[target]! + height + GROUP_GAP;
    return target;
  });
}

// planBounds measures the world a plan produces: each stack is as wide as
// its widest group and as tall as its groups plus the gaps between them.
function planBounds(counts: number[], columns: number, groupColumns: number): { w: number; h: number } {
  const heights = counts.map(count => groupHeight(count, columns));
  const widths = counts.map(count => groupWidth(count, columns));
  const stackOf = stackAssignment(heights, groupColumns);
  const stackW = new Array<number>(groupColumns).fill(0);
  const stackH = new Array<number>(groupColumns).fill(0);
  counts.forEach((_, i) => {
    const stack = stackOf[i]!;
    stackW[stack] = Math.max(stackW[stack]!, widths[i]!);
    stackH[stack] = stackH[stack] === 0 ? heights[i]! : stackH[stack]! + GROUP_GAP + heights[i]!;
  });
  return {
    w: stackW.reduce((sum, width) => sum + width, 0) + (groupColumns - 1) * GROUP_GAP,
    h: Math.max(0, ...stackH),
  };
}

export function layoutPlan(hosts: { address: string }[], width: number, height: number, routeWidth: number): LayoutPlan {
  const counts = [...groupedAddresses(hosts).values()].map(addresses => addresses.length);
  if (counts.length === 0 || width <= 0 || height <= 0) return { columns: 3, groupColumns: 1 };
  const maxCount = Math.max(...counts);
  let best: LayoutPlan = { columns: 1, groupColumns: 1 };
  let bestScale = 0;
  for (let columns = 1; columns <= Math.min(MAX_HOST_COLUMNS, maxCount); columns++) {
    for (let groupColumns = 1; groupColumns <= counts.length; groupColumns++) {
      const bounds = planBounds(counts, columns, groupColumns);
      const scale = Math.min(width / (bounds.w + routeWidth), height / bounds.h);
      if (scale > bestScale) {
        best = { columns, groupColumns };
        bestScale = scale;
      }
    }
  }
  return best;
}

export function pivotPath(from: Position, to: Position, lane: number): string {
  const rowLane = from.y + 18;
  const direction = to.y < rowLane ? -1 : 1;
  const exit = to.x < lane ? -1 : 1;
  const radius = Math.min(8, Math.abs(to.y - rowLane) / 2);
  return `M ${from.x} ${from.y} V ${rowLane - 8} Q ${from.x} ${rowLane} ${from.x + 8} ${rowLane} H ${lane - radius} Q ${lane} ${rowLane} ${lane} ${rowLane + direction * radius} V ${to.y - direction * radius} Q ${lane} ${to.y} ${lane + exit * radius} ${to.y} H ${to.x}`;
}
