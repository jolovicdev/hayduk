import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import type { HostState } from "../protocol/types";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { autorouteRemove, parseRouteTarget, removeRouteItems, runAutoroute } from "../views/pivot";
import { serviceOpen } from "../views/serviceState";
import { osBadge } from "../views/os";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
import { openContextMenuFor } from "./contextmenu";
import {
  NH,
  NW,
  cidrCovers,
  fitLabel,
  geometrySignature,
  layoutHosts,
  layoutPlan,
  layoutWithHeld,
  pivotPath,
  subnetGroups,
  subnetKey,
  type GroupBounds,
  type LayoutPlan,
  type Position,
} from "./graph/layout";
import { loadPositions, pruneStale, savePositions } from "./graph/positions";

const GHOST_W = 220, GHOST_H = 100, GHOST_GAP = 18;
// below this zoom the host cards render smaller than their own text; the
// map switches to readable subnet summaries instead
const LOD_SCALE = 0.3;

interface Viewport extends Position { s: number }

interface Ghost extends Position {
  label: string;
}

interface RouteEdge {
  sessionID: string;
  subnet: string;
  from: Position;
  to: Position;
  lane: number;
  label: Position;
  labelW: number;
}

interface WorldBounds extends Position { w: number; h: number }

// "SESSION <id>" chips sized to their text so longer ids do not overflow
function routeLabelWidth(sessionId: string): number {
  return Math.max(64, 40 + sessionId.length * 7);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function TopologyGraph(props: {
  selected: () => string | undefined;
  onSelect: (addr: string | undefined) => void;
  onInteract: (sid: string) => void;
  onLaunch: (host: string) => void;
  onLogin: (host: string) => void;
}) {
  let svgEl!: SVGSVGElement;
  let resizeObserver: ResizeObserver | undefined;
  let fitFrame = 0;

  const [view, setView] = createSignal<Viewport>({ x: 0, y: 0, s: 1 });
  const [sticky, setSticky] = createSignal(loadPositions());
  const [canvas, setCanvas] = createSignal({ width: 900, height: 500 });
  // the scale the last fit settled on; drives the dense-map hint. Zoom and
  // pan by the operator do not change it.
  const [fittedScale, setFittedScale] = createSignal(0);
  // Manual zoom, pan, or drag suspends automatic fitting and layout changes
  // so discovery does not move hosts the operator is viewing.
  const [userMoved, setUserMoved] = createSignal(false);

  const hosts = createMemo(() => campaignState().hosts.filter((h): h is HostState => !!h));
  let heldPlan: LayoutPlan = { columns: 3, groupColumns: 1 };
  const plan = createMemo(() => {
    const routeWidth = campaignState().routes.length > 0 ? GHOST_W + 100 : 0;
    hosts(), canvas();
    if (userMoved()) return heldPlan;
    heldPlan = layoutPlan(hosts(), Math.max(16, canvas().width - 48), Math.max(16, canvas().height - 48), routeWidth);
    return heldPlan;
  });
  // Preserve existing host positions during manual navigation. Newly
  // discovered hosts receive free slots until the operator fits again.
  const positions = createMemo<Map<string, Position>>(prev => {
    const columns = plan().columns;
    const groupColumns = plan().groupColumns;
    if (userMoved() && prev) {
      const held = new Map(prev);
      for (const [address, position] of sticky()) held.set(address, position);
      return layoutWithHeld(hosts(), held, columns, groupColumns);
    }
    return layoutHosts(hosts(), sticky(), columns, groupColumns);
  });
  const serviceCounts = createMemo(() => {
    const counts = new Map<string, number>();
    for (const service of campaignState().services) {
      if (service && serviceOpen(service)) counts.set(service.host, (counts.get(service.host) ?? 0) + 1);
    }
    return counts;
  });
  const groups = createMemo(() => subnetGroups(hosts(), positions()));

  // Subnet chips keep a constant screen size below the LOD threshold.
  // Place overlapping chips in rows, then columns when the canvas is full.
  const chipLayout = createMemo(() => {
    const camera = view();
    const placed: { x1: number; y1: number; x2: number; y2: number }[] = [];
    const out = new Map<string, { x: number; y: number }>();
    if (camera.s >= LOD_SCALE) return out;
    const limit = canvas().height - 20;
    const halfW = 78, halfH = 16;
    const chips = [...groups()].map(([key, group]) => ({
      key,
      x: camera.x + (group.x + group.w / 2) * camera.s,
      y: camera.y + (group.y + group.h / 2) * camera.s,
    })).sort((a, b) => a.y - b.y);
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
  });

  const access = createMemo(() => {
    const sessions = sessionsByHostMemo();
    const credentials = credsByHostMemo();
    return (address: string) => ({
      sessions: sessions.get(address) ?? [],
      login: (credentials.get(address)?.length ?? 0) > 0,
    });
  });

  // Every route paired with the host groups its CIDR actually covers -
  // strict membership, so a /8 reaches every group inside it and an
  // unparseable or non-IPv4 route reaches none instead of sweeping the
  // shared "other" rail into ROUTED.
  const routedGroups = createMemo(() => {
    const state = campaignState();
    const pairs: { route: { subnet: string; sessionId: string }; covered: Set<string> }[] = [];
    for (const route of state.routes) {
      if (!route?.subnet || !route.sessionId) continue;
      const covered = new Set<string>();
      for (const host of hosts()) {
        if (cidrCovers(route.subnet, host.address)) covered.add(subnetKey(host.address));
      }
      pairs.push({ route: { subnet: route.subnet, sessionId: route.sessionId }, covered });
    }
    return pairs;
  });

  const routedKeys = createMemo(() => {
    const keys = new Set<string>();
    for (const { covered } of routedGroups()) {
      for (const key of covered) keys.add(key);
    }
    return keys;
  });

  const ghosts = createMemo(() => {
    const result = new Map<string, Ghost>();
    const groupMap = groups();
    const hostPositions = positions();
    const state = campaignState();
    const right = Math.max(0, ...[...groupMap.values()].map(group => group.x + group.w)) + 100;

    for (const { route, covered } of routedGroups()) {
      // a route that reaches discovered hosts draws real edges to those
      // groups; only an entirely undiscovered network gets a ghost card
      if (covered.size > 0 || result.has(route.subnet)) continue;

      const session = state.sessions[route.sessionId];
      const sourceHost = session?.targetHost || session?.sessionHost;
      const source = sourceHost ? hostPositions.get(sourceHost) : undefined;
      let y = source ? source.y + NH / 2 - GHOST_H / 2 : result.size * (GHOST_H + GHOST_GAP);
      const previous = [...result.values()].at(-1);
      if (previous) y = Math.max(y, previous.y + GHOST_H + GHOST_GAP);
      result.set(route.subnet, { label: route.subnet, x: right, y });
    }

    return result;
  });

  const routes = createMemo<RouteEdge[]>(() => {
    const state = campaignState();
    const hostPositions = positions();
    const groupMap = groups();
    const ghostMap = ghosts();
    const result: RouteEdge[] = [];

    for (const { route, covered } of routedGroups()) {
      const session = state.sessions[route.sessionId];
      const sourceHost = session?.targetHost || session?.sessionHost;
      const source = sourceHost ? hostPositions.get(sourceHost) : undefined;
      if (!source) continue;
      const from = { x: source.x + NW / 2, y: source.y + NH };
      const lane = Math.max(...[...groupMap.values()].map(group => group.x + group.w)) + 48 + result.length * 14;

      let drawn = false;
      for (const key of covered) {
        const group = groupMap.get(key);
        if (!group) continue;
        const to = { x: group.x + group.w, y: group.y + 30 };
        result.push({
          sessionID: route.sessionId,
          subnet: route.subnet,
          from,
          to,
          lane,
          label: { x: lane, y: to.y - 30 },
          labelW: routeLabelWidth(route.sessionId),
        });
        drawn = true;
      }
      if (drawn) continue;

      const ghost = ghostMap.get(route.subnet);
      if (ghost) {
        const to = { x: ghost.x, y: ghost.y + GHOST_H / 2 };
        result.push({
          sessionID: route.sessionId,
          subnet: route.subnet,
          from,
          to,
          lane,
          label: { x: ghost.x + GHOST_W / 2, y: ghost.y - 30 },
          labelW: routeLabelWidth(route.sessionId),
        });
      }
    }

    return result;
  });

  const worldBounds = createMemo<WorldBounds | undefined>(() => {
    const boxes = [
      ...[...groups().values()].map(group => ({ x: group.x, y: group.y, w: group.w, h: group.h })),
      ...[...ghosts().values()].map(ghost => ({ x: ghost.x, y: ghost.y, w: GHOST_W, h: GHOST_H })),
    ];
    if (boxes.length === 0) return undefined;

    let minX = Math.min(...boxes.map(box => box.x));
    let minY = Math.min(...boxes.map(box => box.y));
    let maxX = Math.max(...boxes.map(box => box.x + box.w));
    let maxY = Math.max(...boxes.map(box => box.y + box.h));
    for (const route of routes()) {
      maxX = Math.max(maxX, route.lane + 10, route.label.x + route.labelW / 2);
      minX = Math.min(minX, route.label.x - route.labelW / 2);
      minY = Math.min(minY, route.label.y);
      minX = Math.min(minX, route.from.x, route.to.x);
      minY = Math.min(minY, route.from.y, route.to.y);
      maxX = Math.max(maxX, route.from.x, route.to.x);
      maxY = Math.max(maxY, route.from.y + 18, route.to.y);
    }
    return { x: minX, y: minY, w: maxX - minX, h: maxY - minY };
  });

  function fit() {
    if (!svgEl) return;
    // Recompute the automatic layout before measuring its bounds.
    setUserMoved(false);
    const bounds = worldBounds();
    if (!bounds) return;
    const rect = svgEl.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const pad = { left: 24, right: 24, top: 24, bottom: 24 };
    // Keep the usable dimensions positive when the stage is smaller than
    // its padding; a negative scale would flip the graph.
    const availW = Math.max(16, rect.width - pad.left - pad.right);
    const availH = Math.max(16, rect.height - pad.top - pad.bottom);
    const scale = Math.min(availW / bounds.w, availH / bounds.h, 1.12);
    setView({
      s: scale,
      x: pad.left + (availW - bounds.w * scale) / 2 - bounds.x * scale,
      y: pad.top + (availH - bounds.h * scale) / 2 - bounds.y * scale,
    });
    setFittedScale(scale);
  }

  // Zoom above the LOD threshold so the subnet's host cards are visible.
  function focusZone(group: GroupBounds) {
    if (!svgEl) return;
    const rect = svgEl.getBoundingClientRect();
    const scale = Math.max(
      LOD_SCALE + 0.01,
      Math.min(rect.width / group.w, rect.height / group.h, 1.12),
    );
    setUserMoved(true);
    setView({
      s: scale,
      x: rect.width / 2 - (group.x + group.w / 2) * scale,
      y: rect.height / 2 - (group.y + group.h / 2) * scale,
    });
  }

  function scheduleFit() {
    cancelAnimationFrame(fitFrame);
    fitFrame = requestAnimationFrame(fit);
  }

  function zoom(multiplier: number, x?: number, y?: number) {
    if (!svgEl) return;
    const rect = svgEl.getBoundingClientRect();
    const focusX = x ?? rect.width / 2;
    const focusY = y ?? rect.height / 2;
    const current = view();
    // the floor sits below any fitted scale so zooming out from a huge
    // campaign's fit never jumps the view
    const scale = clamp(current.s * multiplier, 0.005, 4);
    setUserMoved(true);
    setView({
      s: scale,
      x: focusX - (focusX - current.x) * (scale / current.s),
      y: focusY - (focusY - current.y) * (scale / current.s),
    });
  }

  function onWheel(event: WheelEvent) {
    event.preventDefault();
    const rect = svgEl.getBoundingClientRect();
    zoom(event.deltaY < 0 ? 1.12 : 1 / 1.12, event.clientX - rect.left, event.clientY - rect.top);
  }

  function toWorld(clientX: number, clientY: number): Position {
    const rect = svgEl.getBoundingClientRect();
    const current = view();
    return {
      x: (clientX - rect.left - current.x) / current.s,
      y: (clientY - rect.top - current.y) / current.s,
    };
  }

  // Engine events report the route outcome. A rejected launch must also
  // display its command error.
  function removeRoute(route: { subnet: string; sessionId: string }) {
    runAutoroute(autorouteRemove(route.sessionId, parseRouteTarget(route.subnet)))
      .catch((e: any) => flash(e?.message ?? `could not remove route ${route.subnet}`));
  }

  function edgeMenu(edge: RouteEdge, event: MouseEvent) {
    openContextMenuFor(event,
      removeRouteItems([{ subnet: edge.subnet, sessionId: edge.sessionID }], removeRoute));
  }

  // a routed-network card stands for one unrouted subnet's worth of pivot;
  // offer every session routing it, since several pivots can cover the same
  // network
  function ghostMenu(subnet: string, event: MouseEvent) {
    const routes = campaignState().routes
      .filter(r => !!r && r.subnet === subnet)
      .map(r => ({ subnet: r!.subnet, sessionId: r!.sessionId }));
    openContextMenuFor(event, removeRouteItems(routes, removeRoute));
  }

  function nodeMenu(address: string, event: MouseEvent) {
    props.onSelect(address);
    const hostAccess = access()(address);
    const items: Parameters<typeof openContextMenuFor>[1] = [{ head: address, sub: "host" }];
    for (const session of hostAccess.sessions) {
      items.push({
        icon: "terminal-window",
        label: `Interact with session ${session.id}`,
        fn: () => props.onInteract(session.id),
      });
    }
    items.push(
      { icon: "rocket-launch", label: "Run exploit…", fn: () => props.onLaunch(address) },
      { icon: "key", label: "Login as…", fn: () => props.onLogin(address) },
      { sep: true },
      { icon: "copy", label: "Copy address", hint: address, fn: () => copyWithFeedback(address) },
    );
    openContextMenuFor(event, items);
  }

  interface PanState { startX: number; startY: number; viewX: number; viewY: number; moved: boolean }
  interface DragState { address: string; dx: number; dy: number; startX: number; startY: number; moved: boolean; origin: Position; snapshot: Map<string, Position> }
  interface ChipTap { key: string; startX: number; startY: number; moved: boolean }
  let pan: PanState | undefined;
  let drag: DragState | undefined;
  let chipTap: ChipTap | undefined;

  function onPointerDown(event: PointerEvent) {
    if (event.button !== 0) return;
    const chip = (event.target as Element).closest<SVGGElement>(".topo-zone-chip");
    if (chip?.dataset.zone) {
      chipTap = { key: chip.dataset.zone, startX: event.clientX, startY: event.clientY, moved: false };
    }
    const node = (event.target as Element).closest<SVGGElement>(".topo-node");
    if (node?.dataset.address) {
      const point = toWorld(event.clientX, event.clientY);
      const position = positions().get(node.dataset.address);
      if (!position) return;
      drag = {
        address: node.dataset.address,
        dx: point.x - position.x,
        dy: point.y - position.y,
        startX: event.clientX,
        startY: event.clientY,
        moved: false,
        origin: { x: position.x, y: position.y },
        snapshot: new Map(sticky()),
      };
    } else if (!chipTap) {
      const current = view();
      pan = {
        startX: event.clientX,
        startY: event.clientY,
        viewX: current.x,
        viewY: current.y,
        moved: false,
      };
      svgEl.classList.add("panning");
    }
    svgEl.setPointerCapture(event.pointerId);
  }

  function onPointerMove(event: PointerEvent) {
    if (chipTap) {
      if (Math.abs(event.clientX - chipTap.startX) + Math.abs(event.clientY - chipTap.startY) > 4) chipTap.moved = true;
      return;
    }
    if (drag) {
      if (Math.abs(event.clientX - drag.startX) + Math.abs(event.clientY - drag.startY) > 4) drag.moved = true;
      if (!drag.moved) return;
      setUserMoved(true);
      // zones are bounding boxes of their nodes, so the node can move
      // freely and its zone follows - single-host zones included
      const point = toWorld(event.clientX, event.clientY);
      const position = { x: point.x - drag.dx, y: point.y - drag.dy };
      setSticky(previous => new Map(previous).set(drag!.address, position));
      return;
    }

    if (pan) {
      if (Math.abs(event.clientX - pan.startX) + Math.abs(event.clientY - pan.startY) > 4) pan.moved = true;
      if (pan.moved) setUserMoved(true);
      setView(current => ({
        ...current,
        x: pan!.viewX + event.clientX - pan!.startX,
        y: pan!.viewY + event.clientY - pan!.startY,
      }));
    }
  }

  function endPointer() {
    if (chipTap && !chipTap.moved) {
      const group = groups().get(chipTap.key);
      if (group) focusZone(group);
    }
    if (drag && !drag.moved) props.onSelect(drag.address);
    if (drag?.moved) {
      // forget hosts that left the workspace; storage failures are fine
      const live = new Set(hosts().map(h => h.address));
      const pruned = pruneStale(sticky(), live);
      setSticky(pruned);
      savePositions(pruned);
    }
    if (pan && !pan.moved) props.onSelect(undefined);
    chipTap = undefined;
    drag = undefined;
    pan = undefined;
    svgEl.classList.remove("panning");
  }

  // a cancelled pointer (touch takeover, palm, stylus) is not a click and
  // not a dropped node: abort and restore the pre-drag layout
  function cancelPointer() {
    if (drag) {
      // the pre-drag slot must return even for hosts that had no saved
      // position: the held-positions memo would otherwise keep the
      // dragged coordinates, so the origin is pinned over the snapshot
      const restored = new Map(drag.snapshot);
      if (!restored.has(drag.address)) restored.set(drag.address, drag.origin);
      setSticky(restored);
    }
    chipTap = undefined;
    drag = undefined;
    pan = undefined;
    svgEl.classList.remove("panning");
  }

  onMount(() => {
    const fitEvent = () => fit();
    const zoomEvent = (event: Event) => zoom((event as CustomEvent).detail?.k ?? 1);
    window.addEventListener("hayduk:fit", fitEvent);
    window.addEventListener("hayduk:zoom", zoomEvent);
    resizeObserver = new ResizeObserver(() => {
      const rect = svgEl.getBoundingClientRect();
      setCanvas({ width: rect.width, height: rect.height });
      // Resizing must preserve the operator's suspension of automatic fitting.
      if (!userMoved()) scheduleFit();
    });
    resizeObserver.observe(svgEl);
    scheduleFit();

    onCleanup(() => {
      cancelAnimationFrame(fitFrame);
      resizeObserver?.disconnect();
      window.removeEventListener("hayduk:fit", fitEvent);
      window.removeEventListener("hayduk:zoom", zoomEvent);
    });
  });

  // The signature excludes resource updates that do not affect geometry.
  // Manual navigation suspends fitting until the operator explicitly fits again.
  const geometry = createMemo(() => {
    const state = campaignState();
    return geometrySignature(state.hosts, state.routes, state.sessions);
  });
  createEffect(on(geometry, () => {
    if (!userMoved()) scheduleFit();
  }, { defer: true }));

  return (
    <>
    <div class="map-context">
      <span><span class="map-context-dot"></span>{groups().size} subnet{groups().size === 1 ? "" : "s"}<span class="map-context-divider">/</span>{hosts().length} hosts</span>
      <Show when={hosts().length > 0 && fittedScale() > 0 && fittedScale() < 0.1}>
        <span class="map-context-hint" role="status">
          dense map at this size — zoom into a subnet, or use the Services view
        </span>
      </Show>
      <button onClick={() => { setSticky(new Map()); savePositions(new Map()); scheduleFit(); }} title="Arrange hosts to fit the canvas; replaces saved positions">
        <i aria-hidden="true" class="ph ph-graph"></i>Arrange hosts
      </button>
    </div>
    <svg
      id="topo"
      ref={svgEl}
      role="group"
      aria-label="Campaign network topology"
      classList={{ lod: view().s < LOD_SCALE }}
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endPointer}
      onPointerCancel={cancelPointer}
    >
      <defs>
        <filter id="topo-node-shadow" x="-30%" y="-35%" width="160%" height="180%">
          <feDropShadow dx="0" dy="6" stdDeviation="7" flood-color="#000000" flood-opacity=".32" />
        </filter>
        <marker id="topo-route-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth">
          <path d="M 0 0 L 8 4 L 0 8 z" />
        </marker>
      </defs>

      <Show when={worldBounds()}>
        <g transform={`translate(${view().x} ${view().y}) scale(${view().s})`}>
          <g class="topo-zones">
            <For each={[...groups().keys()]}>{(key) => {
              const group = () => groups().get(key)!;
              return (
              <g class="topo-zone">
                <rect class="topo-zone-panel" x={group().x} y={group().y} width={group().w} height={group().h} rx="18" />
                <path class="topo-zone-rule" d={`M ${group().x + 20} ${group().y + 49} H ${group().x + group().w - 20}`} />
                <g transform={`translate(${group().x + 20} ${group().y + 15})`}>
                  <rect class="topo-zone-icon-bg" width="28" height="24" rx="6" />
                  <circle class="topo-zone-icon" cx="8" cy="12" r="2" />
                  <circle class="topo-zone-icon" cx="19" cy="7" r="2" />
                  <circle class="topo-zone-icon" cx="19" cy="17" r="2" />
                  <path class="topo-zone-link" d="M 10 11 L 17 8 M 10 13 L 17 16" />
                </g>
                <text class="topo-zone-name" x={group().x + 60} y={group().y + 31}>
                  {key === "other" ? "OTHER HOSTS" : `${key}.0/24`}
                </text>
                <text class="topo-zone-meta" x={group().x + group().w - 20} y={group().y + 31} text-anchor="end">
                  {group().count} {group().count === 1 ? "HOST" : "HOSTS"}
                </text>
                <Show when={routedKeys().has(key)}>
                  <text class="topo-zone-route-note" x={group().x + 60} y={group().y + 44}>ROUTED NETWORK</text>
                  <circle class="topo-route-port" cx={group().x + group().w} cy={group().y + 30} r="4" />
                </Show>

              </g>
              );}}</For>
          </g>

          <g class="topo-routes">
            <For each={routes()}>{route => (
              <g onContextMenu={event => edgeMenu(route, event)}>
                <path class="topo-route-halo" d={pivotPath(route.from, route.to, route.lane)} />
                <path class="topo-route-line" d={pivotPath(route.from, route.to, route.lane)} marker-end="url(#topo-route-arrow)" />
                <g class="topo-route-label" transform={`translate(${route.label.x} ${route.label.y})`}>
                  <rect x={-route.labelW / 2} width={route.labelW} height="20" rx="5" />
                  <text x="0" y="13">SESSION {route.sessionID}</text>
                </g>
              </g>
            )}</For>
          </g>

          <g class="topo-hosts">
            <For each={hosts().map(h => h.address)}>{address => {
              const host = () => hosts().find(h => h.address === address)!;
              const position = () => positions().get(address);
              const hostAccess = () => access()(address);
              const badge = () => osBadge(host());
              return (
                <Show when={position()}>{point => (
                  <g
                    class="topo-node"
                    classList={{
                      selected: props.selected() === address,
                      access: hostAccess().sessions.length > 0,
                      login: hostAccess().login && hostAccess().sessions.length === 0,
                    }}
                    data-address={address}
                    transform={`translate(${point().x} ${point().y})`}
                    tabIndex={0}
                    role="button"
                    aria-label={`${host().name || address}, ${address}`}
                    onContextMenu={event => nodeMenu(address, event)}
                    onKeyDown={event => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault(); // Space would scroll the stage
                        props.onSelect(address);
                      }
                    }}
                  >
                    <title>{`${host().name || address} · ${address}`}</title>
                    <rect class="topo-node-shell" width={NW} height={NH} rx="12" />
                    <path class="topo-node-divider" d={`M 0 86 H ${NW}`} />
                    <g class={`topo-device ${badge().key}`} transform="translate(16 16)">
                      <rect class="topo-device-bg" width="32" height="32" rx="8" />
                      <rect class="topo-device-screen" x="8.5" y="7.5" width="15" height="11" rx="2" />
                      <path class="topo-device-stand" d="M 13 23 H 19 M 16 18.5 V 23" />
                    </g>
                    <text class={`topo-node-os ${badge().key}`} x="60" y="28">{fitLabel(badge().label, 14)}</text>
                    <text class="topo-node-name" x="60" y="48">{fitLabel(host().name || address, 19)}</text>
                    <text class="topo-node-address" x="16" y="72">{fitLabel(address, 25)}</text>
                    <circle class="topo-host-status" cx="18" cy="105" r="3" />
                    <text class="topo-host-state" x="28" y="109">
                      {hostAccess().sessions.length > 0 ? `${hostAccess().sessions.length} live session${hostAccess().sessions.length === 1 ? "" : "s"}` : hostAccess().login ? "Login available" : "Discovered"}
                    </text>
                    <text class="topo-host-services" x={NW - 14} y="109" text-anchor="end">
                      {serviceCounts().get(address) ?? 0} service{serviceCounts().get(address) === 1 ? "" : "s"}
                    </text>
                  </g>
                )}</Show>
              );
            }}</For>
          </g>

          <g class="topo-ghosts">
            <For each={Array.from(ghosts().keys())}>{(key) => (
              <g class="topo-ghost" transform={`translate(${ghosts().get(key)!.x} ${ghosts().get(key)!.y})`}
                onContextMenu={event => ghostMenu(key, event)}>
                <rect class="topo-ghost-shell" width={GHOST_W} height={GHOST_H} rx="12" />
                <g transform="translate(14 28)">
                  <circle class="topo-ghost-icon-bg" cx="19" cy="19" r="19" />
                  <circle class="topo-ghost-icon" cx="12" cy="19" r="2.5" />
                  <circle class="topo-ghost-icon" cx="25" cy="12" r="2.5" />
                  <circle class="topo-ghost-icon" cx="25" cy="26" r="2.5" />
                  <path class="topo-ghost-link" d="M 14 18 L 23 13 M 14 20 L 23 25" />
                </g>
                <text class="topo-ghost-kicker" x="62" y="27">ROUTED NETWORK</text>
                <text class="topo-ghost-address" x="62" y="51">{fitLabel(ghosts().get(key)!.label, 19)}</text>
                <text class="topo-ghost-meta" x="62" y="74">AWAITING DISCOVERY</text>
              </g>
            )}</For>
          </g>
        </g>
      </Show>

      <Show when={chipLayout().size > 0}>
        {/* Subnet keys preserve chip elements and focus across updates.
            Handle taps on pointer-up because SVG pointer capture redirects clicks. */}
        <g class="topo-chips">
          <For each={[...groups().keys()]}>{(key) => {
            const chip = () => chipLayout().get(key);
            const count = () => groups().get(key)?.count ?? 0;
            return <Show when={chip()}>{(c) => (
              <g class="topo-zone-chip" role="button" tabIndex={0} data-zone={key}
                aria-label={`Zoom to ${key === "other" ? "other hosts" : `subnet ${key}.0/24`}`}
                transform={`translate(${c().x} ${c().y})`}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault(); // Space would scroll the stage
                    const group = groups().get(key);
                    if (group) focusZone(group);
                  }
                }}>
                <rect class="topo-zone-chip-hit" x="-78" y="-16" width="156" height="32" rx="8" fill="transparent" />
                <text class="topo-zone-chip-name" text-anchor="middle" y="4">
                  {key === "other" ? "OTHER HOSTS" : `${key}.0/24`} · {count()}
                </text>
              </g>
            )}</Show>;
          }}</For>
        </g>
      </Show>
    </svg>
    <output class="map-scale" aria-label="Graph zoom">{Math.round(view().s * 100)}%</output>
    </>
  );
}
