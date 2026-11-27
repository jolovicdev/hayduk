import type { Position } from "./layout";

export interface WorldBox {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

export function toggleHostSelection(
  current: ReadonlySet<string>,
  address: string,
  additive: boolean,
): Set<string> {
  if (!additive) return new Set([address]);
  const next = new Set(current);
  if (next.has(address)) next.delete(address);
  else next.add(address);
  return next;
}

export function pruneHostSelection(
  current: ReadonlySet<string>,
  liveAddresses: ReadonlySet<string>,
): Set<string> {
  const next = new Set<string>();
  for (const addr of current) {
    if (liveAddresses.has(addr)) next.add(addr);
  }
  return next;
}

// hostsInMarquee returns every host card whose bounding rectangle intersects
// the dragged world-space marquee box.
export function hostsInMarquee(
  positions: ReadonlyMap<string, Position>,
  box: WorldBox,
  nodeW: number,
  nodeH: number,
): string[] {
  const left = Math.min(box.x1, box.x2);
  const right = Math.max(box.x1, box.x2);
  const top = Math.min(box.y1, box.y2);
  const bottom = Math.max(box.y1, box.y2);
  const hit: string[] = [];
  for (const [address, pos] of positions) {
    if (pos.x + nodeW >= left && pos.x <= right && pos.y + nodeH >= top && pos.y <= bottom) {
      hit.push(address);
    }
  }
  return hit;
}

export function formatRhosts(addresses: readonly string[]): string {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of addresses) {
    const addr = raw.trim();
    if (addr && !seen.has(addr)) {
      seen.add(addr);
      out.push(addr);
    }
  }
  return out.join(" ");
}
