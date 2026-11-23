import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import type { HostState } from "../protocol/types";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { autorouteRemove, parseRouteTarget, removeRouteItems, runAutoroute } from "../views/pivot";
import { serviceOpen } from "../views/serviceState";
import { osBadge } from "../views/os";
import { OsHardwareGlyph, detectOsFamily } from "./OsIcon";
import { flash } from "../statusflash";
import { errorMessage } from "../errors";
import { copyWithFeedback } from "../clipboard";
import { openContextMenuFor } from "./contextmenu";
import {
  NH,
  NW,
  fitLabel,
  geometrySignature,
  layoutHosts,
  layoutPlan,
  layoutWithHeld,
  pivotPath,
  subnetGroups,
  type GroupBounds,
  type LayoutPlan,
  type Position,
} from "./graph/layout";
import {
  GHOST_H,
  GHOST_W,
  LOD_SCALE,
  computeChipLayout,
  computeGhosts,
  computeRouteEdges,
  computeRoutedGroups,
  computeWorldBounds,
  type RouteEdge,
  type Viewport,
} from "./graph/routing";
import { loadPositions, pruneStale, savePositions } from "./graph/positions";
import { clamp } from "../stores/panels";

export function TopologyGraph(props: {
  selected: () => string | undefined;
  onSelect: (addr: string | undefined) => void;
  onInteract: (sid: string) => void;
  onLaunch: (host: string) => void;
  onLogin: (host: string) => void;
  onPivot: (sid: string) => void;
  onScanSubnet: (target: string) => void;
}) {
  let svgEl!: SVGSVGElement;
  let resizeObserver: ResizeObserver | undefined;
  let fitFrame = 0;
  let moveFrame = 0;

  const [view, setView] = createSignal<Viewport>({ x: 0, y: 0, s: 1 });
  const [sticky, setSticky] = createSignal(loadPositions());
  const [dragPosition, setDragPosition] = createSignal<(Position & { address: string }) | null>(null);
  const [canvas, setCanvas] = createSignal({ width: 900, height: 500 });
  const [fittedScale, setFittedScale] = createSignal(0);
  const [userMoved, setUserMoved] = createSignal(false);

  const hosts = createMemo(() => campaignState().hosts.filter((h): h is HostState => !!h));
  const hostsByAddress = createMemo(() => new Map(hosts().map(h => [h.address, h] as const)));
  const hostGeometry = createMemo(() => hosts().map(h => h.address).join("\x1f"));
  const hostSlots = createMemo(() => {
    const key = hostGeometry();
    return key === "" ? [] : key.split("\x1f").map(address => ({ address }));
  });

  let heldPlan: LayoutPlan = { columns: 3, groupColumns: 1 };
  const plan = createMemo(() => {
    const routeWidth = campaignState().routes.length > 0 ? GHOST_W + 100 : 0;
    hostSlots(), canvas();
    if (userMoved()) return heldPlan;
    heldPlan = layoutPlan(hostSlots(), Math.max(16, canvas().width - 48), Math.max(16, canvas().height - 48), routeWidth);
    return heldPlan;
  });

  const positions = createMemo<Map<string, Position>>(prev => {
    const columns = plan().columns;
    const groupColumns = plan().groupColumns;
    if (userMoved() && prev) {
      const held = new Map(prev);
      for (const [address, position] of sticky()) held.set(address, position);
      return layoutWithHeld(hostSlots(), held, columns, groupColumns);
    }
    return layoutHosts(hostSlots(), sticky(), columns, groupColumns);
  });

  const serviceCounts = createMemo(() => {
    const counts = new Map<string, number>();
    for (const service of campaignState().services) {
      if (service && serviceOpen(service)) counts.set(service.host, (counts.get(service.host) ?? 0) + 1);
    }
    return counts;
  });

  const groups = createMemo(() => subnetGroups(hostSlots(), positions()));
  const chipLayout = createMemo(() => computeChipLayout(view(), groups(), canvas().height));

  const access = createMemo(() => {
    const sessions = sessionsByHostMemo();
    const credentials = credsByHostMemo();
    return (address: string) => ({
      sessions: sessions.get(address) ?? [],
      login: (credentials.get(address)?.length ?? 0) > 0,
    });
  });

  const routedGroups = createMemo(() => computeRoutedGroups(campaignState().routes, hostSlots()));
  const ghosts = createMemo(() => computeGhosts(routedGroups(), groups(), positions(), campaignState().sessions));
  const routes = createMemo(() => computeRouteEdges(routedGroups(), groups(), positions(), ghosts(), campaignState().sessions));
  const routedKeys = createMemo(() => {
    const keys = new Set<string>();
    for (const edge of routes()) {
      if (edge.group) keys.add(edge.group);
    }
    return keys;
  });
  const worldBounds = createMemo(() => computeWorldBounds(groups(), ghosts(), routes()));

  function fit() {
    if (!svgEl) return;
    setUserMoved(false);
    const bounds = worldBounds();
    if (!bounds) return;
    const rect = svgEl.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const pad = 24;
    const availW = Math.max(16, rect.width - pad * 2);
    const availH = Math.max(16, rect.height - pad * 2);
    const scale = Math.min(availW / bounds.w, availH / bounds.h, 1.12);
    setView({
      s: scale,
      x: pad + (availW - bounds.w * scale) / 2 - bounds.x * scale,
      y: pad + (availH - bounds.h * scale) / 2 - bounds.y * scale,
    });
    setFittedScale(scale);
  }

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

  function removeRoute(route: { subnet: string; sessionId: string }) {
    runAutoroute(autorouteRemove(route.sessionId, parseRouteTarget(route.subnet)))
      .catch((e) => flash(errorMessage(e) || `could not remove route ${route.subnet}`));
  }

  function edgeMenu(edge: RouteEdge, event: MouseEvent) {
    const removal = removeRouteItems([{ subnet: edge.subnet, sessionId: edge.sessionID }], removeRoute, false);
    openContextMenuFor(event, [
      { head: edge.subnet, sub: "pivot route" },
      { icon: "wifi-high", label: "Scan this subnet…", fn: () => props.onScanSubnet(edge.subnet) },
      { sep: true },
      ...removal,
    ]);
  }

  function ghostMenu(subnet: string, event: MouseEvent) {
    const matched = campaignState().routes
      .filter(r => !!r && r.subnet === subnet)
      .map(r => ({ subnet: r!.subnet, sessionId: r!.sessionId }));
    const removal = removeRouteItems(matched, removeRoute, false);
    openContextMenuFor(event, [
      { head: subnet, sub: "routed network" },
      { icon: "wifi-high", label: "Scan this subnet…", fn: () => props.onScanSubnet(subnet) },
      { sep: true },
      ...removal,
    ]);
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
      items.push({
        icon: "signpost",
        label: `Pivot via session ${session.id}…`,
        fn: () => props.onPivot(session.id),
      });
    }
    items.push(
      { icon: "wifi-high", label: "Scan services…", fn: () => props.onScanSubnet(address) },
      { icon: "rocket-launch", label: "Launch…", fn: () => props.onLaunch(address) },
      { icon: "key", label: "Login as…", fn: () => props.onLogin(address) },
      { sep: true },
      { icon: "copy", label: "Copy address", hint: address, fn: () => copyWithFeedback(address) },
    );
    openContextMenuFor(event, items);
  }

  interface PanState { startX: number; startY: number; viewX: number; viewY: number; moved: boolean }
  interface DragState {
    address: string; dx: number; dy: number; startX: number; startY: number;
    moved: boolean; rectLeft: number; rectTop: number;
  }
  interface ChipTap { key: string; startX: number; startY: number; moved: boolean }
  interface GhostTap { subnet: string; startX: number; startY: number; moved: boolean }
  let pan: PanState | undefined;
  let drag: DragState | undefined;
  let chipTap: ChipTap | undefined;
  let ghostTap: GhostTap | undefined;
  let pendingClient: { x: number; y: number } | null = null;

  function applyPendingMove() {
    moveFrame = 0;
    const pt = pendingClient;
    if (!pt) return;
    pendingClient = null;
    if (drag?.moved) {
      const current = view();
      const wx = (pt.x - drag.rectLeft - current.x) / current.s;
      const wy = (pt.y - drag.rectTop - current.y) / current.s;
      setDragPosition({ address: drag.address, x: wx - drag.dx, y: wy - drag.dy });
      return;
    }
    if (pan?.moved) {
      setView(current => ({
        ...current,
        x: pan!.viewX + pt.x - pan!.startX,
        y: pan!.viewY + pt.y - pan!.startY,
      }));
    }
  }

  function onPointerDown(event: PointerEvent) {
    if (event.button !== 0) return;
    const chip = (event.target as Element).closest<SVGGElement>(".topo-zone-chip");
    if (chip?.dataset.zone) {
      chipTap = { key: chip.dataset.zone, startX: event.clientX, startY: event.clientY, moved: false };
    }
    const ghost = !chipTap ? (event.target as Element).closest<SVGGElement>(".topo-ghost") : null;
    if (ghost?.dataset.ghost) {
      ghostTap = { subnet: ghost.dataset.ghost, startX: event.clientX, startY: event.clientY, moved: false };
    }
    const node = !chipTap && !ghostTap ? (event.target as Element).closest<SVGGElement>(".topo-node") : null;
    if (node?.dataset.address) {
      const rect = svgEl.getBoundingClientRect();
      const current = view();
      const wx = (event.clientX - rect.left - current.x) / current.s;
      const wy = (event.clientY - rect.top - current.y) / current.s;
      const position = positions().get(node.dataset.address);
      if (!position) return;
      drag = {
        address: node.dataset.address,
        dx: wx - position.x,
        dy: wy - position.y,
        startX: event.clientX,
        startY: event.clientY,
        moved: false,
        rectLeft: rect.left,
        rectTop: rect.top,
      };
    } else if (!chipTap && !ghostTap) {
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
    if (ghostTap) {
      if (Math.abs(event.clientX - ghostTap.startX) + Math.abs(event.clientY - ghostTap.startY) > 4) ghostTap.moved = true;
      return;
    }
    if (drag) {
      if (!drag.moved && Math.abs(event.clientX - drag.startX) + Math.abs(event.clientY - drag.startY) > 4) {
        drag.moved = true;
        setUserMoved(true);
      }
      if (!drag.moved) return;
      pendingClient = { x: event.clientX, y: event.clientY };
      if (!moveFrame) moveFrame = requestAnimationFrame(applyPendingMove);
      return;
    }
    if (pan) {
      if (!pan.moved && Math.abs(event.clientX - pan.startX) + Math.abs(event.clientY - pan.startY) > 4) {
        pan.moved = true;
        setUserMoved(true);
      }
      if (!pan.moved) return;
      pendingClient = { x: event.clientX, y: event.clientY };
      if (!moveFrame) moveFrame = requestAnimationFrame(applyPendingMove);
    }
  }

  function endPointer() {
    if (moveFrame) {
      cancelAnimationFrame(moveFrame);
      applyPendingMove();
    }
    if (chipTap && !chipTap.moved) {
      const group = groups().get(chipTap.key);
      if (group) focusZone(group);
    }
    if (ghostTap && !ghostTap.moved) {
      props.onScanSubnet(ghostTap.subnet);
    }
    if (drag && !drag.moved) props.onSelect(drag.address);
    if (drag?.moved) {
      const dropped = dragPosition();
      const committed = new Map(sticky());
      if (dropped && dropped.address === drag.address) {
        committed.set(dropped.address, { x: dropped.x, y: dropped.y });
      }
      const live = new Set(hosts().map(h => h.address));
      const pruned = pruneStale(committed, live);
      setSticky(pruned);
      savePositions(pruned);
      setDragPosition(null);
    }
    if (pan && !pan.moved) props.onSelect(undefined);
    chipTap = undefined;
    ghostTap = undefined;
    drag = undefined;
    pan = undefined;
    pendingClient = null;
    svgEl.classList.remove("panning");
  }

  function cancelPointer() {
    if (moveFrame) {
      cancelAnimationFrame(moveFrame);
      moveFrame = 0;
    }
    setDragPosition(null);
    chipTap = undefined;
    ghostTap = undefined;
    drag = undefined;
    pan = undefined;
    pendingClient = null;
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
      if (!userMoved()) scheduleFit();
    });
    resizeObserver.observe(svgEl);
    scheduleFit();

    onCleanup(() => {
      cancelAnimationFrame(fitFrame);
      cancelAnimationFrame(moveFrame);
      resizeObserver?.disconnect();
      window.removeEventListener("hayduk:fit", fitEvent);
      window.removeEventListener("hayduk:zoom", zoomEvent);
    });
  });

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
          <filter id="topo-node-shadow" x="-15%" y="-15%" width="130%" height="145%">
            <feDropShadow dx="0" dy="2" stdDeviation="3" flood-color="#000000" flood-opacity=".42" />
          </filter>
          <marker id="topo-route-arrow" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto" markerUnits="strokeWidth">
            <path d="M 0 0 L 7 3.5 L 0 7 z" />
          </marker>
        </defs>

        <Show when={worldBounds()}>
          <g transform={`translate(${view().x} ${view().y}) scale(${view().s})`}>
            <g class="topo-zones">
              <For each={[...groups().keys()]}>{(key) => {
                const group = () => groups().get(key)!;
                return (
                  <g class="topo-zone">
                    <rect class="topo-zone-panel" x={group().x} y={group().y} width={group().w} height={group().h} rx="8" />
                    <rect class="topo-zone-band" x={group().x + 1} y={group().y + 1} width={group().w - 2} height="44" rx="7" />
                    <path class="topo-zone-rule" d={`M ${group().x} ${group().y + 45} H ${group().x + group().w}`} />
                    <g transform={`translate(${group().x + 15} ${group().y + 12})`}>
                      <circle class="topo-zone-icon" cx="4" cy="11" r="2" />
                      <circle class="topo-zone-icon" cx="14" cy="6" r="2" />
                      <circle class="topo-zone-icon" cx="14" cy="16" r="2" />
                      <path class="topo-zone-link" d="M 5.8 10.1 L 12.2 6.9 M 5.8 11.9 L 12.2 15.1" />
                    </g>
                    <text class="topo-zone-name" x={group().x + 40} y={group().y + 27}>
                      {key === "other" ? "OTHER HOSTS" : `${key}.0/24`}
                    </text>
                    <text class="topo-zone-meta" x={group().x + group().w - 16} y={group().y + 27} text-anchor="end">
                      {group().count} {group().count === 1 ? "HOST" : "HOSTS"}
                    </text>
                    <Show when={routedKeys().has(key)}>
                      <text class="topo-zone-route-note" x={group().x + 40} y={group().y + 40}>ROUTED VIA PIVOT</text>
                      <circle class="topo-route-port" cx={group().x + group().w} cy={group().y + 23} r="4" />
                    </Show>
                  </g>
                );
              }}</For>
            </g>

            <g class="topo-routes">
              <For each={routes()}>{route => (
                <g onContextMenu={event => edgeMenu(route, event)}>
                  <path class="topo-route-halo" d={pivotPath(route.from, route.to, route.lane)} />
                  <path class="topo-route-line" d={pivotPath(route.from, route.to, route.lane)} marker-end="url(#topo-route-arrow)" />
                  <g class="topo-route-label" transform={`translate(${route.label.x} ${route.label.y})`}>
                    <rect x={-route.labelW / 2} width={route.labelW} height="20" rx="4" />
                    <text x="0" y="13.5">SESSION {route.sessionID}</text>
                  </g>
                </g>
              )}</For>
            </g>

            <g class="topo-hosts">
              <For each={hosts().map(h => h.address)}>{address => {
                const host = () => hostsByAddress().get(address)!;
                const position = () => {
                  const live = dragPosition();
                  return live && live.address === address ? live : positions().get(address);
                };
                const hostAccess = () => access()(address);
                const badge = () => osBadge(host());
                const family = () => detectOsFamily(host());
                const nodeState = () =>
                  hostAccess().sessions.length > 0 ? "access" : hostAccess().login ? "login" : "idle";
                const osText = () => fitLabel(badge().label === "WIN" ? "WINDOWS" : badge().label, 12);
                const svcCount = () => serviceCounts().get(address) ?? 0;
                return (
                  <Show when={position()}>{point => (
                    <g
                      class="topo-node"
                      classList={{
                        selected: props.selected() === address,
                        access: nodeState() === "access",
                        login: nodeState() === "login",
                      }}
                      data-address={address}
                      transform={`translate(${point().x} ${point().y})`}
                      tabIndex={0}
                      role="button"
                      aria-label={`${host().name || address}, ${address}`}
                      onContextMenu={event => nodeMenu(address, event)}
                      onKeyDown={event => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          props.onSelect(address);
                        }
                      }}
                    >
                      <title>{`${host().name || address} · ${address}`}</title>
                      <rect class="topo-node-shell" width={NW} height={NH} rx="7" />
                      <rect class="topo-node-footer" x="1" y="88" width={NW - 2} height="35" rx="6" />
                      <path class="topo-node-divider" d={`M 1 88 H ${NW - 1}`} />
                      <g transform="translate(14 18)">
                        <OsHardwareGlyph family={family()} state={nodeState()} />
                      </g>
                      <text class={`topo-node-os ${badge().key}`} x="66" y="29">{osText()}</text>
                      <text class="topo-node-svcs-top" x={NW - 13} y="29" text-anchor="end">
                        {svcCount()} {svcCount() === 1 ? "PORT" : "PORTS"}
                      </text>
                      <text class="topo-node-name" x="66" y="49">{fitLabel(host().name || address, 18)}</text>
                      <text class="topo-node-address" x="66" y="68">{fitLabel(address, 19)}</text>
                      <circle class="topo-host-status" cx="19" cy="106" r="3.4" />
                      <text class="topo-host-state" x="29" y="109.5">
                        {hostAccess().sessions.length > 0
                          ? `${hostAccess().sessions.length} LIVE SESSION${hostAccess().sessions.length === 1 ? "" : "S"}`
                          : hostAccess().login ? "LOGIN AVAILABLE" : "DISCOVERED"}
                      </text>
                    </g>
                  )}</Show>
                );
              }}</For>
            </g>

            <g class="topo-ghosts">
              <For each={Array.from(ghosts().keys())}>{(key) => (
                <g class="topo-ghost" data-ghost={key} transform={`translate(${ghosts().get(key)!.x} ${ghosts().get(key)!.y})`}
                  role="button"
                  tabIndex={0}
                  aria-label={`Scan routed network ${ghosts().get(key)!.label}`}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      props.onScanSubnet(key);
                    }
                  }}
                  onContextMenu={event => ghostMenu(key, event)}>
                  <title>{`Click to scan ${ghosts().get(key)!.label} · Right-click for route options`}</title>
                  <rect class="topo-ghost-shell" width={GHOST_W} height={GHOST_H} rx="7" />
                  <g transform="translate(14 31)">
                    <rect class="topo-ghost-icon-bg" width="32" height="32" rx="6" />
                    <circle class="topo-ghost-icon" cx="10" cy="16" r="2.2" />
                    <circle class="topo-ghost-icon" cx="22" cy="10" r="2.2" />
                    <circle class="topo-ghost-icon" cx="22" cy="22" r="2.2" />
                    <path class="topo-ghost-link" d="M 12 15 L 20 11 M 12 17 L 20 21" />
                  </g>
                  <text class="topo-ghost-kicker" x="58" y="29">ROUTED NETWORK</text>
                  <text class="topo-ghost-address" x="58" y="52">{fitLabel(ghosts().get(key)!.label, 19)}</text>
                  <text class="topo-ghost-meta" x="58" y="75">Click to scan subnet →</text>
                </g>
              )}</For>
            </g>
          </g>
        </Show>

        <Show when={chipLayout().size > 0}>
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
                      e.preventDefault();
                      const group = groups().get(key);
                      if (group) focusZone(group);
                    }
                  }}>
                  <rect class="topo-zone-chip-hit" x="-78" y="-16" width="156" height="32" rx="4" fill="transparent" />
                  <text class="topo-zone-chip-name" text-anchor="middle" y="4">
                    {key === "other" ? "Other hosts" : `${key}.0/24`} · {count()}
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
