import type { RouteState, SessionState } from "../../protocol/types";
import { NH, NW, cidrCovers, subnetKey, type GroupBounds, type Position } from "./layout";

export const GHOST_W = 220;
export const GHOST_H = 100;
export const GHOST_GAP = 18;
// Below this zoom the host cards render smaller than their own text; the
// map switches to readable subnet summaries instead.
export const LOD_SCALE = 0.3;

export interface Viewport extends Position {
  s: number;
}

export interface Ghost extends Position {
  label: string;
}

export interface RouteEdge {
  sessionID: string;
  subnet: string;
  // group is the subnet-group key this edge lands on; "" for ghost edges,
  // which land on a routed-network card instead of a host group.
  group: string;
  from: Position;
  to: Position;
  lane: number;
  label: Position;
  labelW: number;
}

export interface WorldBounds extends Position {
  w: number;
  h: number;
}

export interface RoutedGroupPair {
  route: { subnet: string; sessionId: string };
  covered: Set<string>;
}

// "SESSION <id>" chips sized to their text so longer ids do not overflow.
export function routeLabelWidth(sessionId: string): number {
  return Math.max(64, 40 + sessionId.length * 7);
}

// Every route paired with the host groups its CIDR actually covers -
// strict membership, so a /8 reaches every group inside it and an
// unparseable or non-IPv4 route reaches none instead of sweeping the
// shared "other" rail into ROUTED.
export function computeRoutedGroups(
  routes: readonly (RouteState | null | undefined)[],
  hosts: readonly { address: string }[],
): RoutedGroupPair[] {
  const pairs: RoutedGroupPair[] = [];
  for (const route of routes) {
    if (!route?.subnet || !route.sessionId) continue;
    const covered = new Set<string>();
    for (const host of hosts) {
      if (cidrCovers(route.subnet, host.address)) covered.add(subnetKey(host.address));
    }
    pairs.push({ route: { subnet: route.subnet, sessionId: route.sessionId }, covered });
  }
  return pairs;
}

export function computeGhosts(
  routed: readonly RoutedGroupPair[],
  groups: ReadonlyMap<string, GroupBounds>,
  positions: ReadonlyMap<string, Position>,
  sessions: Readonly<Record<string, SessionState | undefined>>,
): Map<string, Ghost> {
  const result = new Map<string, Ghost>();
  const right = Math.max(0, ...[...groups.values()].map(group => group.x + group.w)) + 100;

  for (const { route, covered } of routed) {
    if (covered.size > 0 || result.has(route.subnet)) continue;
    const session = sessions[route.sessionId];
    const sourceHost = session?.targetHost || session?.sessionHost;
    const source = sourceHost ? positions.get(sourceHost) : undefined;
    let y = source ? source.y + NH / 2 - GHOST_H / 2 : result.size * (GHOST_H + GHOST_GAP);
    const previous = [...result.values()].at(-1);
    if (previous) y = Math.max(y, previous.y + GHOST_H + GHOST_GAP);
    result.set(route.subnet, { label: route.subnet, x: right, y });
  }

  return result;
}

export function computeRouteEdges(
  routed: readonly RoutedGroupPair[],
  groups: ReadonlyMap<string, GroupBounds>,
  positions: ReadonlyMap<string, Position>,
  ghosts: ReadonlyMap<string, Ghost>,
  sessions: Readonly<Record<string, SessionState | undefined>>,
): RouteEdge[] {
  const result: RouteEdge[] = [];
  const rightEdge = Math.max(0, ...[...groups.values()].map(group => group.x + group.w));
  const nextLane = () => rightEdge + 48 + result.length * 14;

  for (const { route, covered } of routed) {
    const session = sessions[route.sessionId];
    const sourceHost = session?.targetHost || session?.sessionHost;
    const source = sourceHost ? positions.get(sourceHost) : undefined;
    if (!source) continue;
    const from = { x: source.x + NW / 2, y: source.y + NH };

    let drawn = false;
    for (const key of covered) {
      const group = groups.get(key);
      if (!group) continue;
      const lane = nextLane();
      const to = { x: group.x + group.w, y: group.y + 30 };
      result.push({
        sessionID: route.sessionId,
        subnet: route.subnet,
        group: key,
        from,
        to,
        lane,
        label: { x: lane, y: to.y - 30 },
        labelW: routeLabelWidth(route.sessionId),
      });
      drawn = true;
    }
    if (drawn) continue;

    const ghost = ghosts.get(route.subnet);
    if (ghost) {
      const lane = nextLane();
      const to = { x: ghost.x, y: ghost.y + GHOST_H / 2 };
      result.push({
        sessionID: route.sessionId,
        subnet: route.subnet,
        group: "",
        from,
        to,
        lane,
        label: { x: ghost.x + GHOST_W / 2, y: ghost.y - 30 },
        labelW: routeLabelWidth(route.sessionId),
      });
    }
  }

  return result;
}

export function computeWorldBounds(
  groups: ReadonlyMap<string, GroupBounds>,
  ghosts: ReadonlyMap<string, Ghost>,
  routes: readonly RouteEdge[],
): WorldBounds | undefined {
  const boxes = [
    ...[...groups.values()].map(group => ({ x: group.x, y: group.y, w: group.w, h: group.h })),
    ...[...ghosts.values()].map(ghost => ({ x: ghost.x, y: ghost.y, w: GHOST_W, h: GHOST_H })),
  ];
  if (boxes.length === 0) return undefined;

  let minX = Math.min(...boxes.map(box => box.x));
  let minY = Math.min(...boxes.map(box => box.y));
  let maxX = Math.max(...boxes.map(box => box.x + box.w));
  let maxY = Math.max(...boxes.map(box => box.y + box.h));
  for (const route of routes) {
    maxX = Math.max(maxX, route.lane + 10, route.label.x + route.labelW / 2);
    minX = Math.min(minX, route.label.x - route.labelW / 2);
    minY = Math.min(minY, route.label.y);
    minX = Math.min(minX, route.from.x, route.to.x);
    minY = Math.min(minY, route.from.y, route.to.y);
    maxX = Math.max(maxX, route.from.x, route.to.x);
    maxY = Math.max(maxY, route.from.y + 18, route.to.y);
  }
  return { x: minX, y: minY, w: maxX - minX, h: maxY - minY };
}

// Subnet chips keep a constant screen size below the LOD threshold.
// Place overlapping chips in rows, then columns when the canvas is full.
export function computeChipLayout(
  camera: Viewport,
  groups: ReadonlyMap<string, GroupBounds>,
  canvasHeight: number,
): Map<string, Position> {
  const out = new Map<string, Position>();
  if (camera.s >= LOD_SCALE) return out;
  const placed: { x1: number; y1: number; x2: number; y2: number }[] = [];
  const limit = canvasHeight - 20;
  const halfW = 78;
  const halfH = 16;
  const chips = [...groups]
    .map(([key, group]) => ({
      key,
      x: camera.x + (group.x + group.w / 2) * camera.s,
      y: camera.y + (group.y + group.h / 2) * camera.s,
    }))
    .sort((a, b) => a.y - b.y);
  for (const chip of chips) {
    for (let column = 0; column < 6; column++) {
      const x = chip.x + column * 170;
      let y = column === 0 ? chip.y : 16;
      while (placed.some(r =>
        x - halfW < r.x2 && x + halfW > r.x1 && y - halfH < r.y2 && y + halfH > r.y1,
      )) y += 34;
      if (y + halfH <= limit || column === 5) {
        y = Math.min(y, Math.max(16, limit - halfH));
        placed.push({ x1: x - halfW, y1: y - halfH, x2: x + halfW, y2: y + halfH });
        out.set(chip.key, { x, y });
        break;
      }
    }
  }
  return out;
}
