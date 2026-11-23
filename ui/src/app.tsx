import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import ConnectDialog from "./views/ConnectDialog";
import { ContextMenuRoot, closeContextMenu, openContextMenu } from "./components/contextmenu";
import { Dropdown, MenuItemButton } from "./components/dropdown";
import { Modal } from "./components/modal";
import { ModuleTree } from "./components/ModuleTree";
import { ConsoleView } from "./components/ConsoleView";
import { TopologyGraph } from "./components/TopologyGraph";
import { CommandPalette } from "./components/CommandPalette";
import { SessionsView } from "./views/SessionsView";
import { JobsView } from "./views/JobsView";
import { OperationsView } from "./views/OperationsView";
import { CredsView } from "./views/CredsView";
import { LootView } from "./views/LootView";
import { EventsView } from "./views/EventsView";
import { ServicesView } from "./views/ServicesView";
import { HostsView } from "./views/HostsView";
import { ListenersView } from "./views/ListenersView";
import { Inspector } from "./views/Inspector";
import { InteractView } from "./views/InteractView";
import { LaunchDialog } from "./views/LaunchDialog";
import { PivotDialog } from "./views/PivotDialog";
import { ScanDialog } from "./views/ScanDialog";
import { LoginDialog } from "./views/LoginDialog";
import { FindAttacksDialog } from "./views/FindAttacksDialog";
import { HailMaryDialog } from "./views/HailMaryDialog";
import { BruteDialog } from "./views/BruteDialog";
import { runningCount } from "./views/operations";
import { campaignState, protoMismatch, serverVersion, team as teamMode, wsStatus } from "./stores/store";
import { consoleBusy, consoleOutput, consolePrompt, tabs as consoleTabs, write } from "./stores/console";
import { attach, resetInteract } from "./stores/interact";
import { markChatRead, unreadChat } from "./stores/events";
import { ws } from "./ws/singleton";
import { currentOperator } from "./ws/client";
import { forgetOperator, restoreOperator, saveOperator } from "./ws/operator";
import { flash } from "./statusflash";
import { errorMessage } from "./errors";
import { HaydukMark } from "./components/mark";
import { switchWorkspaceAction } from "./appActions";
import { DEFAULT_PANELS, clamp, clampNotebook, clampPanels, type PanelSizes } from "./stores/panels";

const notebookTabs: [string, string, string][] = [
  ["console", "terminal-window", "Console"],
  ["interact", "terminal", "Interact"],
  ["sessions", "broadcast", "Sessions"],
  ["listeners", "plug", "Listeners"],
  ["jobs", "gear", "Jobs"],
  ["ops", "pulse", "Operations"],
  ["creds", "key", "Credentials"],
  ["loot", "archive", "Loot"],
  ["events", "chat", "Events"],
];

function loadPanels(): PanelSizes {
  try {
    const raw = localStorage.getItem("hayduk.panels");
    if (raw) return clampPanels(JSON.parse(raw), window.innerHeight);
  } catch {
    // corrupt sizes fall back to defaults
  }
  return { ...DEFAULT_PANELS };
}

function Splitter(props: { area: "lsp" | "rsp" | "nsp"; onDelta: (dx: number, dy: number) => void; onEnd?: () => void }) {
  let el!: HTMLDivElement;
  const [active, setActive] = createSignal(false);
  let last: { x: number; y: number } | null = null;
  return (
    <div
      ref={el}
      class={`splitter ${props.area}`}
      classList={{ active: active() }}
      onPointerDown={(e) => {
        last = { x: e.clientX, y: e.clientY };
        el.setPointerCapture(e.pointerId);
        setActive(true);
      }}
      onPointerMove={(e) => {
        if (!last) return;
        props.onDelta(e.clientX - last.x, e.clientY - last.y);
        last = { x: e.clientX, y: e.clientY };
      }}
      onPointerUp={() => { last = null; setActive(false); props.onEnd?.(); }}
      onPointerCancel={() => { last = null; setActive(false); props.onEnd?.(); }}
    />
  );
}

export default function App() {
  const [tab, setTab] = createSignal("console");
  const [stage, setStage] = createSignal<"topo" | "svc" | "hosts">("topo");
  const [scanTarget, setScanTarget] = createSignal<string | undefined>(undefined);
  const [selectedHost, setSelectedHost] = createSignal<string | undefined>(undefined);
  const [launch, setLaunch] = createSignal<{ type: string; path: string; host?: string; port?: number } | null>(null);
  const [scan, setScan] = createSignal<"discovery" | "services" | null>(null);
  const [loginHost, setLoginHost] = createSignal<string | undefined>(undefined);
  const [pivotSid, setPivotSid] = createSignal<string | undefined>(undefined);
  const [findOpen, setFindOpen] = createSignal(false);
  const [hailOpen, setHailOpen] = createSignal(false);
  const [bruteOpen, setBruteOpen] = createSignal(false);
  const [showAbout, setShowAbout] = createSignal(false);
  const [showKeys, setShowKeys] = createSignal(false);
  const [cmdOpen, setCmdOpen] = createSignal(false);
  const [confirmTeamDisconnect, setConfirmTeamDisconnect] = createSignal(false);
  const [grid, setGrid] = createSignal(true);
  const [graphFocus, setGraphFocus] = createSignal(false);
  const [leftOpen, setLeftOpen] = createSignal(true);
  const [rightOpen, setRightOpen] = createSignal(true);
  const [nbMax, setNbMax] = createSignal(false);
  const [workspaces, setWorkspaces] = createSignal<string[]>([]);
  const [panels, setPanels] = createSignal(loadPanels());
  const [operatorName, setOperatorName] = createSignal(restoreOperator());
  const [operatorDraft, setOperatorDraft] = createSignal("");

  function savePanels() {
    localStorage.setItem("hayduk.panels", JSON.stringify(panels()));
  }
  function resizeLeft(dx: number) {
    setPanels(p => ({ ...p, left: clamp(p.left + dx, 200, 560) }));
  }
  function resizeRight(dx: number) {
    setPanels(p => ({ ...p, right: clamp(p.right - dx, 250, 640) }));
  }
  function resizeNotebook(_dx: number, dy: number) {
    setPanels(p => ({ ...p, nb: clampNotebook(p.nb - dy, window.innerHeight) }));
  }

  const conn = createMemo(() => campaignState().connection);
  const connected = createMemo(() => conn().status === "connected");
  const sessions = createMemo(() => campaignState().sessions);
  const jobs = createMemo(() => campaignState().jobs);
  const modules = createMemo(() => campaignState().modules);
  const hosts = createMemo(() => campaignState().hosts);
  const services = createMemo(() => campaignState().services);
  const creds = createMemo(() => campaignState().creds);
  const liveCount = createMemo(() => Object.keys(sessions()).length);
  const jobCount = createMemo(() => Object.keys(jobs()).length);
  const runningOps = createMemo(() => runningCount(campaignState().operations));
  const totalModules = createMemo(() => {
    const m = modules();
    if (!m) return 0;
    return [m.exploits, m.auxiliary, m.post, m.payloads, m.encoders, m.nops, m.evasion]
      .reduce((a, v) => a + (v?.length ?? 0), 0);
  });

  function selectHost(addr: string | undefined) {
    setSelectedHost(addr);
    if (addr && !rightOpen()) setRightOpen(true);
  }

  function openInteract(sid: string) {
    attach(sid).then(
      () => { setGraphFocus(false); setTab("interact"); },
      (e) => flash(errorMessage(e) || `could not attach to session ${sid}`),
    );
  }

  function fit() {
    window.dispatchEvent(new CustomEvent("hayduk:fit"));
  }

  function zoom(dir: number) {
    window.dispatchEvent(new CustomEvent("hayduk:zoom", { detail: { k: dir > 0 ? 1.2 : 1 / 1.2 } }));
  }

  async function loadWorkspaces() {
    try {
      setWorkspaces(await ws.command<string[]>("workspace.list"));
    } catch {
      // workspace list needs a connected msf database; the chip stays empty
    }
  }

  async function openWorkspaceMenu(anchor: HTMLElement) {
    if (!connected()) return;
    await loadWorkspaces();
    const rect = anchor.getBoundingClientRect();
    openContextMenu(rect.left, rect.bottom + 4, [
      { head: "Workspaces", sub: "switch active msf workspace" },
      ...workspaces().map(w => ({
        label: w,
        icon: w === conn().workspace ? "check" : undefined,
        fn: () => void switchWorkspace(w),
      })),
    ]);
  }

  const switchWorkspace = switchWorkspaceAction({
    command: (method, params) => ws.command(method, params),
    onSwitched: () => setSelectedHost(undefined),
    flash,
  });

  createEffect(on(() => conn().workspace, () => setSelectedHost(undefined), { defer: true }));

  function commitOperator() {
    const name = operatorDraft().trim();
    if (!name) return;
    if (name !== currentOperator()) resetInteract();
    saveOperator(name);
    setOperatorName(name);
    void ws.command("operator.join").catch(() => {});
  }

  function disconnect() {
    if (teamMode() && conn().status !== "disconnected") {
      setConfirmTeamDisconnect(true);
      return;
    }
    void ws.command("disconnect").catch(() => {});
  }

  createEffect(() => {
    if (tab() === "events" && unreadChat() > 0) markChatRead();
  });

  async function exportReport() {
    try {
      const { html } = await ws.command<{ html: string }>("report.html");
      const blob = new Blob([html], { type: "text/html" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `hayduk-report-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.html`;
      a.click();
      URL.revokeObjectURL(url);
      flash("report exported");
    } catch (e) {
      flash(errorMessage(e) || "report export failed");
    }
  }

  function runPaletteAction(action: string) {
    if (action === "discover") setScan("discovery");
    else if (action === "act:scan") setScan("services");
    else if (action === "find") setFindOpen(true);
    else if (action === "brute") setBruteOpen(true);
    else if (action === "hail") setHailOpen(true);
    else if (action === "report") void exportReport();
    else if (action === "focus") { setNbMax(false); setGraphFocus(!graphFocus()); }
    else if (action === "fit") { setStage("topo"); fit(); }
    else if (action.startsWith("stage:")) {
      setNbMax(false);
      setStage(action.slice(6) as "topo" | "svc" | "hosts");
    } else if (action.startsWith("tab:")) {
      setGraphFocus(false);
      setTab(action.slice(4));
    }
  }

  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setCmdOpen(v => !v);
        return;
      }
      if (e.key === "Escape") {
        closeContextMenu();
        setShowAbout(false);
        setShowKeys(false);
        setCmdOpen(false);
        return;
      }
      const tag = document.activeElement?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (e.key === "1") { setNbMax(false); setStage("topo"); }
      else if (e.key === "2") { setNbMax(false); setStage("svc"); }
      else if (e.key === "3") { setNbMax(false); setStage("hosts"); }
      else if ((e.key === "f" || e.key === "F") && stage() === "topo") fit();
      else if (e.key === "[") setLeftOpen(v => !v);
      else if (e.key === "]") setRightOpen(v => !v);
    };
    document.addEventListener("keydown", onKey);
    onCleanup(() => document.removeEventListener("keydown", onKey));
  });

  return (
    <>
      <div
        class="app"
        classList={{
          "graph-focused": graphFocus(),
          "no-left": !leftOpen(),
          "no-right": !rightOpen(),
          "nb-max": nbMax() && !graphFocus(),
        }}
        style={{
          "--w-left": `${panels().left}px`,
          "--w-right": `${panels().right}px`,
          "--h-nb": `${panels().nb}px`,
        }}
      >
        <header class="menubar">
          <div class="brand">
            <HaydukMark size={18} />
            <span class="word">hayduk</span>
          </div>

          <nav class="mnav" aria-label="Main menu">
            <Dropdown id="m-file" label="File">
              <MenuItemButton icon="x" label="Disconnect" disabled={conn().status === "disconnected"}
                onClick={() => disconnect()} />
              <div class="dsep"></div>
              <MenuItemButton icon="download-simple" label="Export report…" disabled={!connected()} onClick={() => void exportReport()} />
              <MenuItemButton icon="info" label="About hayduk" onClick={() => setShowAbout(true)} />
            </Dropdown>

            <Dropdown id="m-camp" label="Campaign">
              <MenuItemButton icon="crosshair" label="Discover hosts…" disabled={!connected()}
                onClick={() => setScan("discovery")} />
              <MenuItemButton icon="wifi-high" label="Scan services…" disabled={!connected()}
                onClick={() => setScan("services")} />
              <MenuItemButton icon="key" label="Brute force logins…" disabled={!connected()}
                onClick={() => setBruteOpen(true)} />
              <div class="dsep"></div>
              <MenuItemButton icon="lightning" label="Find attacks…" disabled={!connected()}
                onClick={() => setFindOpen(true)} />
              <div class="dsep"></div>
              <MenuItemButton icon="fire" label="Hail Mary…" disabled={!connected()}
                onClick={() => setHailOpen(true)} />
            </Dropdown>

            <Dropdown id="m-view" label="View">
              <MenuItemButton icon="graph" label="Topology" hint="1" onClick={() => { setNbMax(false); setStage("topo"); }} />
              <MenuItemButton icon="table" label="Services" hint="2" onClick={() => { setNbMax(false); setStage("svc"); }} />
              <MenuItemButton icon="stack" label="Hosts" hint="3" onClick={() => { setNbMax(false); setStage("hosts"); }} />
              <div class="dsep"></div>
              <MenuItemButton icon="stack" label={leftOpen() ? "Hide modules" : "Show modules"} hint="["
                onClick={() => setLeftOpen(v => !v)} />
              <MenuItemButton icon="target" label={rightOpen() ? "Hide inspector" : "Show inspector"} hint="]"
                onClick={() => setRightOpen(v => !v)} />
              <MenuItemButton icon="command" label="Command palette" hint="Ctrl+K"
                onClick={() => setCmdOpen(true)} />
              <div class="dsep"></div>
              <MenuItemButton icon="dots-nine" label={grid() ? "Dot grid: on" : "Dot grid: off"}
                onClick={() => setGrid(!grid())} />
              <MenuItemButton icon="arrows-out-simple" label="Fullscreen" hint="F11"
                onClick={() => {
                  if (document.fullscreenElement) void document.exitFullscreen();
                  else void document.documentElement.requestFullscreen();
                }} />
            </Dropdown>

            <Dropdown id="m-help" label="Help">
              <MenuItemButton icon="keyboard" label="Keyboard and mouse" onClick={() => setShowKeys(true)} />
              <div class="dsep"></div>
              <MenuItemButton icon="info" label="About hayduk" onClick={() => setShowAbout(true)} />
            </Dropdown>
          </nav>

          <span class="spacer"></span>

          <div class="telemetry" aria-label="Campaign telemetry">
            <button class="tstat" onClick={() => { setNbMax(false); setStage("topo"); }} title="View hosts on topology">
              HOSTS <b>{hosts().length}</b>
            </button>
            <button class="tstat" onClick={() => { setNbMax(false); setStage("svc"); }} title="View discovered services">
              SVCS <b>{services().length}</b>
            </button>
            <button class="tstat" classList={{ live: liveCount() > 0 }} onClick={() => { setGraphFocus(false); setTab("sessions"); }} title="View live sessions">
              <span class="tdot"></span>SESSIONS <b>{liveCount()}</b>
            </button>
            <button class="tstat" onClick={() => { setGraphFocus(false); setTab("creds"); }} title="View recovered credentials">
              CREDS <b>{creds().length}</b>
            </button>
          </div>

          <button class="cmd-trigger" onClick={() => setCmdOpen(true)} title="Open command palette (Ctrl+K)">
            <i aria-hidden="true" class="ph ph-magnifying-glass"></i>
            <span>Command</span>
            <kbd>Ctrl+K</kbd>
          </button>

          <div class="bar-actions">
            <button class="tbtn" disabled={!connected()} onClick={() => void exportReport()} title="Export HTML campaign report">
              <i aria-hidden="true" class="ph ph-download-simple"></i>Export
            </button>
            <button class="tbtn" disabled={!connected()} onClick={() => setScan("services")} title="Scan target services">
              <i aria-hidden="true" class="ph ph-wifi-high"></i>Scan
            </button>
            <button class="tbtn primary" disabled={!connected()} onClick={() => setScan("discovery")} title="Discover hosts on target network">
              <i aria-hidden="true" class="ph ph-plus"></i>Discover
            </button>
          </div>

          <Show when={teamMode()}>
            <span class="chip team-chip" title={`Team session: click to change operator name. Online: ${(campaignState().operators ?? []).join(", ") || "nobody"}`}
              onClick={() => {
                const previous = currentOperator();
                forgetOperator(localStorage);
                resetInteract();
                setOperatorName("");
                setOperatorDraft(previous);
              }}>
              <i aria-hidden="true" class="ph ph-users-three"></i>
              <b>{operatorName() || "unnamed"}</b> · {(campaignState().operators ?? []).length}
            </span>
          </Show>

          <button class="chip workspace-chip" disabled={!connected()} title="Active workspace: click to switch"
            onClick={(e) => void openWorkspaceMenu(e.currentTarget as HTMLElement)}>
            <i aria-hidden="true" class="ph ph-stack"></i>
            <b>{conn().workspace || "offline"}</b>
            <i aria-hidden="true" class="ph ph-caret-down"></i>
          </button>
        </header>

        <aside class="left card">
          <div class="panelhead">
            <i aria-hidden="true" class="ph ph-stack"></i>
            <span class="pt">Modules</span>
            <span class="pc">{totalModules().toLocaleString()}</span>
            <button class="ibtn" onClick={() => setLeftOpen(false)} aria-label="Collapse modules panel" title="Hide modules ([)">
              <i aria-hidden="true" class="ph ph-x"></i>
            </button>
          </div>
          <ModuleTree onLaunch={(type, path) => setLaunch({ type, path, host: selectedHost() })} />
        </aside>

        <main class="stage card">
          <div class="stagehead">
            <Show when={!leftOpen() && !graphFocus()}>
              <button class="ibtn" onClick={() => setLeftOpen(true)} aria-label="Show modules panel" title="Show modules ([)">
                <i aria-hidden="true" class="ph ph-stack"></i>
              </button>
            </Show>
            <div class="seg">
              <button aria-pressed={stage() === "topo"} classList={{ on: stage() === "topo" }} onClick={() => setStage("topo")}>
                <i aria-hidden="true" class="ph ph-graph"></i>Topology
              </button>
              <button aria-pressed={stage() === "svc"} classList={{ on: stage() === "svc" }} onClick={() => setStage("svc")}>
                <i aria-hidden="true" class="ph ph-table"></i>Services
              </button>
              <button aria-pressed={stage() === "hosts"} classList={{ on: stage() === "hosts" }} onClick={() => setStage("hosts")}>
                <i aria-hidden="true" class="ph ph-stack"></i>Hosts
              </button>
            </div>
            <span class="spacer"></span>
            <Show when={stage() === "topo"}>
              <div class="zoomui">
                <button class="zbtn" aria-label="Zoom out" onClick={() => zoom(-1)} title="Zoom out"><i aria-hidden="true" class="ph ph-minus"></i></button>
                <button class="zbtn" aria-label="Zoom in" onClick={() => zoom(1)} title="Zoom in"><i aria-hidden="true" class="ph ph-plus"></i></button>
                <button class="zbtn" aria-label="Fit to view" onClick={fit} title="Fit graph to view (F)"><i aria-hidden="true" class="ph ph-corners-out"></i></button>
              </div>
            </Show>
            <button class="graph-focus" aria-pressed={graphFocus()} onClick={() => { setNbMax(false); setGraphFocus(!graphFocus()); }}
              aria-label={graphFocus() ? "Exit focus" : "Focus stage"}
              title={graphFocus() ? "Exit focus mode" : "Maximize stage and inspector"}>
              <i aria-hidden="true" class="ph ph-arrows-out-simple"></i><span class="focus-label">{graphFocus() ? "Exit focus" : "Focus"}</span>
            </button>
            <Show when={!rightOpen()}>
              <button class="ibtn" onClick={() => setRightOpen(true)} aria-label="Show inspector panel" title="Show inspector (])">
                <i aria-hidden="true" class="ph ph-target"></i>
              </button>
            </Show>
          </div>
          <div class="stagebody" classList={{ gridbg: stage() === "topo" && grid() }}>
            <div class="view" id="view-topo" hidden={stage() !== "topo"}>
              <TopologyGraph selected={selectedHost} onSelect={selectHost}
                onInteract={openInteract}
                onLaunch={(host) => {
                  selectHost(host);
                  flash(`host ${host} selected; launch a module from the tree or Ctrl+K`);
                }}
                onLogin={(host) => { selectHost(host); setLoginHost(host); }}
                onPivot={(sid) => setPivotSid(sid)}
                onScanSubnet={(target) => { setScanTarget(target); setScan("services"); }} />
              <Show when={hosts().length === 0}>
                <div class="map-empty">
                  <div class="empty-symbol"><i aria-hidden="true" class="ph ph-graph"></i></div>
                  <h2>No hosts in workspace</h2>
                  <p>Run host discovery, import an Nmap/Nessus scan, or add a host manually.</p>
                  <button class="tbtn primary" disabled={!connected()} onClick={() => setScan("discovery")}>
                    <i aria-hidden="true" class="ph ph-plus"></i>Discover hosts
                  </button>
                </div>
              </Show>
              <div class="legend">
                <span><i class="sw neutral"></i>DISCOVERED</span>
                <span><i class="sw dot"></i>LIVE SESSION</span>
                <span><i class="sw sq"></i>CREDS</span>
                <span><i class="sw dash"></i>PIVOT</span>
              </div>
            </div>
            <div class="view" hidden={stage() !== "svc"}>
              <ServicesView onInspect={(addr) => selectHost(addr)} selected={selectedHost()} />
            </div>
            <div class="view" hidden={stage() !== "hosts"}>
              <HostsView onInspect={(addr) => { selectHost(addr); setStage("topo"); }} selected={selectedHost()} />
            </div>
          </div>
        </main>

        <aside class="right card">
          <div class="panelhead">
            <i aria-hidden="true" class="ph ph-target"></i>
            <span class="pt">Inspector</span>
            <Show when={selectedHost()}>
              {(ip) => <span class="pc">{ip()}</span>}
            </Show>
            <button class="ibtn" style={selectedHost() ? undefined : "margin-left:auto"} onClick={() => setRightOpen(false)} aria-label="Collapse inspector panel" title="Hide inspector (])">
              <i aria-hidden="true" class="ph ph-x"></i>
            </button>
          </div>
          <Inspector addr={selectedHost} onInteract={openInteract} onLogin={setLoginHost} onPivot={setPivotSid} />
        </aside>

        <section class="nb card">
          <div class="nbtabs">
            <For each={notebookTabs}>{([id, icon, label]) => (
              <button class="nbtab" aria-pressed={tab() === id} classList={{ on: tab() === id }} onClick={() => setTab(id)}>
                <i aria-hidden="true" class={`ph ph-${icon}`}></i>{label}
                <Show when={id === "sessions" && liveCount() > 0}>
                  <span class="badge">{liveCount()}</span>
                </Show>
                <Show when={id === "jobs" && jobCount() > 0}>
                  <span class="badge jobs">{jobCount()}</span>
                </Show>
                <Show when={id === "ops" && runningOps() > 0}>
                  <span class="badge jobs" title="Running operations">{runningOps()}</span>
                </Show>
                <Show when={id === "events" && unreadChat() > 0}>
                  <span class="badge chat" title="Unread team chat">{unreadChat()}</span>
                </Show>
              </button>
            )}</For>
            <span class="spacer"></span>
            <button class="ibtn" style="align-self:center; margin-right:4px" aria-pressed={nbMax()}
              onClick={() => setNbMax(v => !v)}
              aria-label={nbMax() ? "Restore bottom dock" : "Maximize bottom dock"}
              title={nbMax() ? "Restore bottom dock" : "Maximize bottom dock"}>
              <i aria-hidden="true" class="ph ph-arrows-out-simple"></i>
            </button>
          </div>
          <div class="nbbody">
            <div class="nbpane" hidden={tab() !== "console"}>
              <ConsoleView output={consoleOutput} prompt={consolePrompt()} busy={consoleBusy()}
                write={(cmd) => void write(cmd).catch((e) => flash(errorMessage(e) || "console write failed"))}
                tabComplete={(line) => consoleTabs(line).catch(() => [])} />
            </div>
            <div class="nbpane" hidden={tab() !== "interact"}><InteractView /></div>
            <div class="nbpane" hidden={tab() !== "sessions"}>
              <SessionsView onInteract={openInteract} active={() => tab() === "sessions"}
                onScanSubnet={(subnet) => { setScanTarget(subnet); setScan("services"); }} />
            </div>
            <div class="nbpane" hidden={tab() !== "listeners"}>
              <ListenersView onInteract={openInteract} active={() => tab() === "listeners"} />
            </div>
            <div class="nbpane" hidden={tab() !== "jobs"}>
              <JobsView active={() => tab() === "jobs"} onOpenModule={(module) => {
                setTab("console");
                void write(`use ${module}`)
                  .catch((e) => flash(errorMessage(e) || "console write failed"));
              }} />
            </div>
            <div class="nbpane" hidden={tab() !== "ops"}>
              <OperationsView active={() => tab() === "ops"} />
            </div>
            <div class="nbpane" hidden={tab() !== "creds"}><CredsView /></div>
            <div class="nbpane" hidden={tab() !== "loot"}><LootView /></div>
            <div class="nbpane" hidden={tab() !== "events"}><EventsView /></div>
          </div>
        </section>

        <footer class="status">
          <i aria-hidden="true" class={`ph ${connected() ? "ph-wifi-high wifi" : "ph-wifi-slash wifi-off"}`}
             title="RPC link to Metasploit"></i>
          <span class={connected() ? "st-live" : ""}>
            {connected()
              ? `msfrpcd · metasploit ${conn().msfVersion}`
              : conn().status === "reconnecting"
                ? "reconnecting…"
                : "disconnected"}
          </span>
          <Show when={conn().host}>
            <span class="addr">{conn().host}:{conn().port}</span>
          </Show>
          <span class="spacer"></span>
          <span id="statusflash" role="status"></span>
          <span class="spacer"></span>
          <span class="ver">HAYDUK {serverVersion() || "…"}</span>
        </footer>

        <Show when={cmdOpen()}>
          <CommandPalette
            connected={connected()}
            onClose={() => setCmdOpen(false)}
            onAction={runPaletteAction}
            onSelectHost={(addr) => { setNbMax(false); selectHost(addr); setStage("topo"); }}
            onInteract={openInteract}
            onLaunchModule={(type, path) => setLaunch({ type, path, host: selectedHost() })}
          />
        </Show>

        <Show when={hailOpen()}>
          <HailMaryDialog host={selectedHost()} onClose={() => setHailOpen(false)} />
        </Show>

        <Show when={bruteOpen()}>
          <BruteDialog host={selectedHost()} onClose={() => setBruteOpen(false)} />
        </Show>

        <Show when={findOpen()}>
          <FindAttacksDialog host={selectedHost()}
            onLaunch={(path, host, port) => {
              setFindOpen(false);
              selectHost(host);
              setLaunch({ type: "exploit", path, host, port });
            }}
            onClose={() => setFindOpen(false)} />
        </Show>

        <Show when={loginHost()}>
          {(h) => <LoginDialog host={h()} onClose={() => setLoginHost(undefined)} />}
        </Show>

        <Show when={pivotSid()}>
          {(sid) => (
            <PivotDialog sid={sid()} meterpreter={sessions()[sid()]?.type === "meterpreter"}
              onScan={(subnet) => { setPivotSid(undefined); setScanTarget(subnet); setScan("services"); }}
              onClose={() => setPivotSid(undefined)} />
          )}
        </Show>

        <Show when={scan()}>
          {(mode) => (
            <ScanDialog mode={mode()} target={scanTarget() ?? selectedHost()}
              onConfigure={(module, target) => {
                setScan(null);
                setLaunch({ type: "auxiliary", path: module, host: target });
              }}
              onClose={() => { setScan(null); setScanTarget(undefined); }} />
          )}
        </Show>

        <Show when={launch()}>
          {(l) => <LaunchDialog type={l().type} path={l().path} prefillHost={l().host} prefillPort={l().port}
            onClose={() => setLaunch(null)} />}
        </Show>

        <Show when={conn().status === "disconnected" || conn().status === "connecting"}>
          <ConnectDialog conn={conn()} onConnect={(p) => ws.command("connect", p)} />
        </Show>

        <Show when={showAbout()}>
          <Modal title="About hayduk" onClose={() => setShowAbout(false)}>
            <div class="about-row">
              <HaydukMark size={40} tile />
              <div>
                <p class="mtext">
                  Graphical attack management console for Metasploit. Runs as a single Go binary
                  with a browser UI. For authorized security testing only.
                </p>
              </div>
            </div>
            <p class="mnote" style="margin-top:12px">
              Hayduk {serverVersion() || ""} · jolovicdev · MIT
            </p>
            <div class="mbtns">
              <button class="abtn" onClick={() => setShowAbout(false)}>Close</button>
            </div>
          </Modal>
        </Show>

        <Show when={showKeys()}>
          <Modal title="Keyboard and mouse" onClose={() => setShowKeys(false)}>
            <div class="klist">
              <div class="krow"><span class="keys"><kbd>Ctrl</kbd><kbd>K</kbd></span>Open command palette</div>
              <div class="krow"><span class="keys"><kbd>1</kbd><kbd>2</kbd><kbd>3</kbd></span>Switch between Topology, Services, and Hosts</div>
              <div class="krow"><span class="keys"><kbd>[</kbd><kbd>]</kbd></span>Toggle Modules and Inspector panels</div>
              <div class="krow"><span class="keys"><kbd>F</kbd></span>Fit the graph to the view</div>
              <div class="krow"><span class="keys"><kbd>Esc</kbd></span>Close menus and dialogs</div>
              <div class="krow"><span class="keys"><kbd>R-click</kbd></span>Context actions on hosts, modules, and table rows</div>
              <div class="krow"><span class="keys"><kbd>Drag</kbd></span>Move nodes, or pan the canvas by its background</div>
              <div class="krow"><span class="keys"><kbd>Wheel</kbd></span>Zoom the graph</div>
              <div class="krow"><span class="keys"><kbd>Tab</kbd></span>Complete console commands</div>
            </div>
            <div class="mbtns">
              <button class="abtn" onClick={() => setShowKeys(false)}>Close</button>
            </div>
          </Modal>
        </Show>

        <Show when={teamMode() && !operatorName()}>
          <Modal title="Who is operating?" onClose={() => setOperatorDraft("")}>
            <p class="mdesc">
              This Hayduk runs as a team server. Your name rides along on every command and lands
              next to your actions in the shared event log.
            </p>
            <div class="mform">
              <input value={operatorDraft()} placeholder="operator name" maxlength={32}
                onInput={(e) => setOperatorDraft(e.currentTarget.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && operatorDraft().trim()) commitOperator(); }}
                autocomplete="off" spellcheck={false} />
            </div>
            <div class="mbtns">
              <button class="abtn primary" disabled={!operatorDraft().trim()} onClick={commitOperator}>Join</button>
            </div>
          </Modal>
        </Show>

        <Show when={confirmTeamDisconnect()}>
          <Modal title="Disconnect the whole team?" onClose={() => setConfirmTeamDisconnect(false)}>
            <p class="mdesc">
              This Hayduk shares one msfrpcd link between all operators. Disconnecting drops
              it for everyone — live sessions, jobs, and consoles go with it. Campaign data
              and the event log stay.
            </p>
            <div class="mbtns">
              <button class="abtn" onClick={() => setConfirmTeamDisconnect(false)}>Keep connected</button>
              <button class="abtn red" onClick={() => {
                setConfirmTeamDisconnect(false);
                void ws.command("disconnect").catch(() => {});
              }}>Disconnect team</button>
            </div>
          </Modal>
        </Show>

        <Show when={wsStatus() === "closed" && !protoMismatch()}>
          <div class="modalback show">
            <div class="modal" style="width:340px; text-align:center">
              <div class="mtitle" style="text-align:center">Connection to Hayduk lost</div>
              <p>Reconnecting automatically…</p>
            </div>
          </div>
        </Show>

        <Show when={protoMismatch()}>
          <div class="modalback show">
            <div class="modal" style="width:360px; text-align:center">
              <div class="mtitle" style="text-align:center">Incompatible Hayduk server</div>
              <p>This UI speaks protocol v1 but the server answered with a different version.
                 Restart both from the same build.</p>
            </div>
          </div>
        </Show>

        <Splitter area="lsp" onDelta={resizeLeft} onEnd={savePanels} />
        <Splitter area="rsp" onDelta={resizeRight} onEnd={savePanels} />
        <Splitter area="nsp" onDelta={resizeNotebook} onEnd={savePanels} />

        <ContextMenuRoot />
      </div>
    </>
  );
}
