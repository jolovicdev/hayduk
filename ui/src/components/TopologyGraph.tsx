import { For, Show, createEffect, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import type { HostState } from "../protocol/types";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { autorouteRemove, parseRouteTarget, removeRouteItems, runAutoroute } from "../views/pivot";
import { osBadge } from "../views/os";
import { openContextMenu } from "./contextmenu";
import {
  GROUP_INSET,
  NH,
  NW,
  fitLabel,
  layoutHosts,
  subnetGroups,
  subnetKey,
  type Position,
} from "./graph/layout";
import { loadPositions, pruneStale, savePositions } from "./graph/positions";

const GHOST_W = 210, GHOST_H = 76, GHOST_GAP = 18;

interface Viewport extends Position { s: number }

interface Ghost extends Position {
  label: string;
}

interface RouteEdge {
  sessionID: string;
  subnet: string;
  from: Position;
  to: Position;
  lane?: number;
  label: Position;
  labelW: number;
}

interface WorldBounds extends Position { w: number; h: number }

// "SESSION <id>" chips sized to their text so longer ids do not overflow
function routeLabelWidth(sessionId: string): number {
  return Math.max(64, 40 + sessionId.length * 7);
}

function routePath(edge: RouteEdge): string {
  if (edge.lane !== undefined) {
    return `M ${edge.from.x} ${edge.from.y} C ${edge.lane} ${edge.from.y}, ${edge.lane} ${edge.to.y}, ${edge.to.x} ${edge.to.y}`;
  }
  const bend = Math.max(44, Math.abs(edge.to.x - edge.from.x) * 0.42);
  return `M ${edge.from.x} ${edge.from.y} C ${edge.from.x + bend} ${edge.from.y}, ${edge.to.x - bend} ${edge.to.y}, ${edge.to.x} ${edge.to.y}`;
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

  const hosts = createMemo(() => campaignState().hosts.filter((h): h is HostState => !!h));
  const positions = createMemo(() => layoutHosts(hosts(), sticky()));
  const groups = createMemo(() => subnetGroups(hosts(), positions()));

  const access = createMemo(() => {
    const sessions = sessionsByHostMemo();
    const credentials = credsByHostMemo();
    return (address: string) => ({
      sessions: sessions.get(address) ?? [],
      login: (credentials.get(address)?.length ?? 0) > 0,
    });
  });

  const routedKeys = createMemo(() => new Set(campaignState().routes
    .filter(route => !!route?.subnet)
    .map(route => subnetKey(route!.subnet.split("/")[0] ?? ""))));

  const ghosts = createMemo(() => {
    const result = new Map<string, Ghost>();
    const groupMap = groups();
    const hostPositions = positions();
    const state = campaignState();
    const right = Math.max(0, ...[...groupMap.values()].map(group => group.x + group.w)) + 84;

    for (const route of state.routes) {
      if (!route?.subnet || !route.sessionId) continue;
      const key = subnetKey(route.subnet.split("/")[0] ?? "");
      if (key === "other" || groupMap.has(key) || result.has(key)) continue;

      const session = state.sessions[route.sessionId];
      const sourceHost = session?.targetHost || session?.sessionHost;
      const source = sourceHost ? hostPositions.get(sourceHost) : undefined;
      let y = source ? source.y + NH / 2 - GHOST_H / 2 : result.size * (GHOST_H + GHOST_GAP);
      const previous = [...result.values()].at(-1);
      if (previous) y = Math.max(y, previous.y + GHOST_H + GHOST_GAP);
      result.set(key, { label: route.subnet, x: right, y });
    }

    return result;
  });

  const routes = createMemo<RouteEdge[]>(() => {
    const state = campaignState();
    const hostPositions = positions();
    const groupMap = groups();
    const ghostMap = ghosts();
    const result: RouteEdge[] = [];

    state.routes.forEach(route => {
      if (!route?.subnet || !route.sessionId) return;
      const session = state.sessions[route.sessionId];
      const sourceHost = session?.targetHost || session?.sessionHost;
      const source = sourceHost ? hostPositions.get(sourceHost) : undefined;
      if (!source) return;

      const key = subnetKey(route.subnet.split("/")[0] ?? "");
      const group = groupMap.get(key);
      const ghost = ghostMap.get(key);
      const from = { x: source.x + NW, y: source.y + NH / 2 };

      if (group) {
        const lane = Math.max(from.x, group.x + group.w) + 64 + result.length * 16;
        const to = { x: group.x + group.w, y: group.y + 14 };
        result.push({
          sessionID: route.sessionId,
          subnet: route.subnet,
          from,
          to,
          lane,
          label: { x: lane - 40, y: (from.y + to.y) / 2 - 11 },
          labelW: routeLabelWidth(route.sessionId),
        });
        return;
      }

      if (ghost) {
        const to = { x: ghost.x, y: ghost.y + GHOST_H / 2 };
        result.push({
          sessionID: route.sessionId,
          subnet: route.subnet,
          from,
          to,
          label: { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 13 },
          labelW: routeLabelWidth(route.sessionId),
        });
      }
    });

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
      if (route.lane !== undefined) maxX = Math.max(maxX, route.lane + 10);
      minX = Math.min(minX, route.from.x, route.to.x);
      minY = Math.min(minY, route.from.y, route.to.y);
      maxX = Math.max(maxX, route.from.x, route.to.x);
      maxY = Math.max(maxY, route.from.y, route.to.y);
    }
    return { x: minX, y: minY, w: maxX - minX, h: maxY - minY };
  });

  function fit() {
    const bounds = worldBounds();
    if (!bounds || !svgEl) return;
    const rect = svgEl.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const pad = { left: 38, right: 38, top: 54, bottom: 58 };
    // no fixed floor: a 500-host campaign must still fit on screen
    const scale = Math.max(0.02, Math.min(
      (rect.width - pad.left - pad.right) / bounds.w,
      (rect.height - pad.top - pad.bottom) / bounds.h,
      1.16,
    ));
    setView({
      s: scale,
      x: pad.left + (rect.width - pad.left - pad.right - bounds.w * scale) / 2 - bounds.x * scale,
      y: pad.top + (rect.height - pad.top - pad.bottom - bounds.h * scale) / 2 - bounds.y * scale,
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
    const scale = clamp(current.s * multiplier, 0.05, 4);
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

  // removing a pivot runs the autoroute module on the session that owns the
  // route; the engine's launch and route-diff events narrate the outcome
  function removeRoute(route: { subnet: string; sessionId: string }) {
    void runAutoroute(autorouteRemove(route.sessionId, parseRouteTarget(route.subnet)));
  }

  function edgeMenu(edge: RouteEdge, event: MouseEvent) {
    event.preventDefault();
    event.stopPropagation();
    openContextMenu(event.clientX, event.clientY,
      removeRouteItems([{ subnet: edge.subnet, sessionId: edge.sessionID }], removeRoute));
  }

  // a routed-network card stands for one subnet; offer every session routing
  // it, since several pivots can cover the same network
  function ghostMenu(key: string, event: MouseEvent) {
    event.preventDefault();
    event.stopPropagation();
    const routes = campaignState().routes
      .filter(r => !!r && subnetKey(r.subnet.split("/")[0] ?? "") === key)
      .map(r => ({ subnet: r!.subnet, sessionId: r!.sessionId }));
    openContextMenu(event.clientX, event.clientY, removeRouteItems(routes, removeRoute));
  }

  function nodeMenu(address: string, event: MouseEvent) {
    event.preventDefault();
    event.stopImmediatePropagation();
    props.onSelect(address);
    const hostAccess = access()(address);
    const items: Parameters<typeof openContextMenu>[2] = [{ head: address, sub: "host" }];
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
      { icon: "copy", label: "Copy address", hint: address, fn: () => navigator.clipboard.writeText(address) },
    );
    openContextMenu(event.clientX, event.clientY, items);
  }

  interface PanState { startX: number; startY: number; viewX: number; viewY: number; moved: boolean }
  interface DragState { address: string; dx: number; dy: number; startX: number; startY: number; moved: boolean; snapshot: Map<string, Position> }
  let pan: PanState | undefined;
  let drag: DragState | undefined;

  function onPointerDown(event: PointerEvent) {
    if (event.button !== 0) return;
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
        snapshot: new Map(sticky()),
      };
    } else {
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
    if (drag) {
      if (Math.abs(event.clientX - drag.startX) + Math.abs(event.clientY - drag.startY) > 4) drag.moved = true;
      if (!drag.moved) return;
      // zones are bounding boxes of their nodes, so the node can move
      // freely and its zone follows - single-host zones included
      const point = toWorld(event.clientX, event.clientY);
      const position = { x: point.x - drag.dx, y: point.y - drag.dy };
      setSticky(previous => new Map(previous).set(drag!.address, position));
      return;
    }

    if (pan) {
      if (Math.abs(event.clientX - pan.startX) + Math.abs(event.clientY - pan.startY) > 4) pan.moved = true;
      setView(current => ({
        ...current,
        x: pan!.viewX + event.clientX - pan!.startX,
        y: pan!.viewY + event.clientY - pan!.startY,
      }));
    }
  }

  function endPointer() {
    if (drag && !drag.moved) props.onSelect(drag.address);
    if (drag?.moved) {
      // forget hosts that left the workspace; storage failures are fine
      const live = new Set(hosts().map(h => h.address));
      const pruned = pruneStale(sticky(), live);
      setSticky(pruned);
      savePositions(pruned);
    }
    if (pan && !pan.moved) props.onSelect(undefined);
    drag = undefined;
    pan = undefined;
    svgEl.classList.remove("panning");
  }

  // a cancelled pointer (touch takeover, palm, stylus) is not a click and
  // not a dropped node: abort and restore the pre-drag layout
  function cancelPointer() {
    if (drag) setSticky(drag.snapshot);
    drag = undefined;
    pan = undefined;
    svgEl.classList.remove("panning");
  }

  onMount(() => {
    const fitEvent = () => fit();
    const zoomEvent = (event: Event) => zoom((event as CustomEvent).detail?.k ?? 1);
    window.addEventListener("hayduk:fit", fitEvent);
    window.addEventListener("hayduk:zoom", zoomEvent);
    resizeObserver = new ResizeObserver(scheduleFit);
    resizeObserver.observe(svgEl);
    scheduleFit();

    onCleanup(() => {
      cancelAnimationFrame(fitFrame);
      resizeObserver?.disconnect();
      window.removeEventListener("hayduk:fit", fitEvent);
      window.removeEventListener("hayduk:zoom", zoomEvent);
    });
  });

  createEffect(() => {
    hosts().map(host => host.address).join("|");
    campaignState().routes.map(route => `${route?.subnet}:${route?.sessionId}`).join("|");
    scheduleFit();
  });

  return (
    <svg
      id="topo"
      ref={svgEl}
      role="group"
      aria-label="Campaign network topology"
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endPointer}
      onPointerCancel={cancelPointer}
    >
      <defs>
        <linearGradient id="topo-node-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stop-color="#1e2634" />
          <stop offset="1" stop-color="#141a23" />
        </linearGradient>
        <filter id="topo-node-shadow" x="-30%" y="-35%" width="160%" height="180%">
          <feDropShadow dx="0" dy="6" stdDeviation="7" flood-color="#000000" flood-opacity=".32" />
        </filter>
        <filter id="topo-node-glow" x="-40%" y="-40%" width="180%" height="180%">
          <feDropShadow dx="0" dy="0" stdDeviation="5" flood-color="#e2666b" flood-opacity=".4" />
        </filter>
        <marker id="topo-route-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth">
          <path d="M 0 0 L 8 4 L 0 8 z" />
        </marker>
      </defs>

      <Show when={worldBounds()} fallback={
        <g class="topo-empty">
          <text x="50%" y="48%">No hosts discovered</text>
          <text class="topo-empty-sub" x="50%" y="54%">Scan a network to build the topology.</text>
        </g>
      }>
        <g transform={`translate(${view().x} ${view().y}) scale(${view().s})`}>
          <g class="topo-zones">
            <For each={[...groups()]}>{([key, group]) => (
              <g class="topo-zone">
                <g transform={`translate(${group.x} ${group.y})`}>
                  <circle class="topo-zone-icon-bg" cx="14" cy="14" r="14" />
                  <circle class="topo-zone-icon" cx="9" cy="14" r="2" />
                  <circle class="topo-zone-icon" cx="19" cy="9" r="2" />
                  <circle class="topo-zone-icon" cx="19" cy="19" r="2" />
                  <path class="topo-zone-link" d="M 11 13 L 17 10 M 11 15 L 17 18" />
                </g>
                <text class="topo-zone-name" x={group.x + 40} y={group.y + 13}>
                  {key === "other" ? "OTHER HOSTS" : `${key}.0/24`}
                </text>
                <text class="topo-zone-meta" x={group.x + 40} y={group.y + 28}>
                  {group.count} {group.count === 1 ? "HOST" : "HOSTS"}
                </text>
                <path class="topo-zone-spine" d={`M ${group.x + 14} ${group.y + 30} V ${group.y + group.h - GROUP_INSET - NH - 15}`} />
                <For each={hosts().filter(host => subnetKey(host.address) === key)}>{host => {
                  const point = () => positions().get(host.address);
                  return <Show when={point()}>{position => (
                    <>
                      <path class="topo-zone-branch" d={`M ${group.x + 14} ${position().y - 15} H ${position().x + NW / 2} V ${position().y}`} />
                      <circle class="topo-zone-junction" cx={position().x + NW / 2} cy={position().y - 15} r="2.5" />
                    </>
                  )}</Show>;
                }}</For>
                <Show when={routedKeys().has(key)}>
                  <g class="topo-zone-routed" transform={`translate(${group.x + group.w - 68} ${group.y + 4})`}>
                    <rect width="62" height="20" rx="5" />
                    <circle cx="11" cy="10" r="2.5" />
                    <text x="19" y="13">ROUTED</text>
                  </g>
                  <circle class="topo-route-port" cx={group.x + group.w} cy={group.y + 14} r="4" />
                </Show>
              </g>
            )}</For>
          </g>

          <g class="topo-routes">
            <For each={routes()}>{route => (
              <g onContextMenu={event => edgeMenu(route, event)}>
                <path class="topo-route-halo" d={routePath(route)} />
                <path class="topo-route-line" d={routePath(route)} marker-end="url(#topo-route-arrow)" />
                <g class="topo-route-label" transform={`translate(${route.label.x} ${route.label.y})`}>
                  <rect x={-route.labelW / 2} width={route.labelW} height="20" rx="5" />
                  <text x="0" y="13">SESSION {route.sessionID}</text>
                </g>
              </g>
            )}</For>
          </g>

          <g class="topo-hosts">
            <For each={hosts()}>{host => {
              const position = () => positions().get(host.address);
              const hostAccess = () => access()(host.address);
              const badge = () => osBadge(host);
              return (
                <Show when={position()}>{point => (
                  <g
                    class="topo-node"
                    classList={{
                      selected: props.selected() === host.address,
                      access: hostAccess().sessions.length > 0,
                      login: hostAccess().login && hostAccess().sessions.length === 0,
                    }}
                    data-address={host.address}
                    transform={`translate(${point().x} ${point().y})`}
                    tabIndex={0}
                    role="button"
                    aria-label={`${host.name || host.address}, ${host.address}`}
                    onContextMenu={event => nodeMenu(host.address, event)}
                    onKeyDown={event => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault(); // Space would scroll the stage
                        props.onSelect(host.address);
                      }
                    }}
                  >
                    <title>{`${host.name || host.address} · ${host.address}`}</title>
                    <rect class="topo-node-shell" width={NW} height={NH} rx="10" />
                    <g class={`topo-device ${badge().key}`} transform="translate(14 13)">
                      <rect class="topo-device-bg" width="28" height="28" rx="7" />
                      <rect class="topo-device-screen" x="6.5" y="6.5" width="15" height="11" rx="2" />
                      <path class="topo-device-stand" d="M 11 21 H 17 M 14 17.5 V 21" />
                    </g>
                    <text class={`topo-node-os ${badge().key}`} x="52" y="21">{fitLabel(badge().label, 14)}</text>
                    <text class="topo-node-name" x="52" y="39">{fitLabel(host.name || host.address, 20)}</text>
                    <circle class="topo-address-dot" cx="18" cy="60" r="2.7" />
                    <text class="topo-node-address" x="27" y="64">{fitLabel(host.address, 16)}</text>
                    <Show when={hostAccess().sessions.length > 0}>
                      <g class="topo-state access" transform={`translate(${NW - 75} 52)`}>
                        <rect width="60" height="18" rx="5" />
                        <circle class="topo-live-halo" cx="11" cy="9" r="3" />
                        <circle class="topo-live-dot" cx="11" cy="9" r="2.6" />
                        <text x="20" y="12">ACCESS</text>
                      </g>
                    </Show>
                    <Show when={hostAccess().login && hostAccess().sessions.length === 0}>
                      <g class="topo-state login" transform={`translate(${NW - 69} 52)`}>
                        <rect width="54" height="18" rx="5" />
                        <circle cx="11" cy="9" r="2.5" />
                        <text x="20" y="12">LOGIN</text>
                      </g>
                    </Show>
                  </g>
                )}</Show>
              );
            }}</For>
          </g>

          <g class="topo-ghosts">
            <For each={Array.from(ghosts().entries())}>{([key, ghost]) => (
              <g class="topo-ghost" transform={`translate(${ghost.x} ${ghost.y})`}
                onContextMenu={event => ghostMenu(key, event)}>
                <rect class="topo-ghost-shell" width={GHOST_W} height={GHOST_H} rx="12" />
                <g transform="translate(16 19)">
                  <circle class="topo-ghost-icon-bg" cx="19" cy="19" r="19" />
                  <circle class="topo-ghost-icon" cx="12" cy="19" r="2.5" />
                  <circle class="topo-ghost-icon" cx="25" cy="12" r="2.5" />
                  <circle class="topo-ghost-icon" cx="25" cy="26" r="2.5" />
                  <path class="topo-ghost-link" d="M 14 18 L 23 13 M 14 20 L 23 25" />
                </g>
                <text class="topo-ghost-kicker" x="64" y="28">ROUTED NETWORK</text>
                <text class="topo-ghost-address" x="64" y="49">{fitLabel(ghost.label, 19)}</text>
                <text class="topo-ghost-meta" x="64" y="64">AWAITING DISCOVERY</text>
              </g>
            )}</For>
          </g>
        </g>
      </Show>
    </svg>
  );
}
