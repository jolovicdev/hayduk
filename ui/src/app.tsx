import { Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import { ContextMenuRoot, closeContextMenu, openContextMenu } from "./components/contextmenu";
import { ModuleTree } from "./components/ModuleTree";
import { TopologyGraph } from "./components/TopologyGraph";
import { Splitter } from "./components/Splitter";
import { TopBar } from "./components/TopBar";
import { NotebookDock } from "./components/NotebookDock";
import { AppModals, type LaunchRequest } from "./views/AppModals";
import { ServicesView } from "./views/ServicesView";
import { HostsView } from "./views/HostsView";
import { Inspector } from "./views/Inspector";
import { runningCount } from "./views/operations";
import { boundIdentity, campaignState, protoMismatch, serverVersion, team as teamMode, wsStatus } from "./stores/store";
import { attach, resetInteract } from "./stores/interact";
import { markChatRead, unreadChat } from "./stores/events";
import { ws } from "./ws/singleton";
import { currentOperator } from "./ws/client";
import { forgetOperator, restoreOperator, saveOperator } from "./ws/operator";
import { flash } from "./statusflash";
import { errorMessage } from "./errors";
import { switchWorkspaceAction } from "./appActions";
import { DEFAULT_PANELS, clamp, clampNotebook, clampPanels, type PanelSizes } from "./stores/panels";

function loadPanels(): PanelSizes {
  try {
    const raw = localStorage.getItem("hayduk.panels");
    if (raw) return clampPanels(JSON.parse(raw), window.innerHeight);
  } catch {
    // corrupt sizes fall back to defaults
  }
  return { ...DEFAULT_PANELS };
}

export default function App() {
  const [tab, setTab] = createSignal("console");
  const [stage, setStage] = createSignal<"topo" | "svc" | "hosts">("topo");
  const [scanTarget, setScanTarget] = createSignal<string | undefined>(undefined);
  const [selectedHost, setSelectedHost] = createSignal<string | undefined>(undefined);
  const [launch, setLaunch] = createSignal<LaunchRequest | null>(null);
  const [scan, setScan] = createSignal<"discovery" | "services" | null>(null);
  const [loginHost, setLoginHost] = createSignal<string | undefined>(undefined);
  const [pivotSid, setPivotSid] = createSignal<string | undefined>(undefined);
  const [findOpen, setFindOpen] = createSignal(false);
  const [hailOpen, setHailOpen] = createSignal(false);
  const [bruteOpen, setBruteOpen] = createSignal(false);
  const [workspaceCreateOpen, setWorkspaceCreateOpen] = createSignal(false);
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
  const operators = createMemo(() => campaignState().operators ?? []);
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

  async function deleteWorkspace(name: string) {
    try {
      await ws.command("workspace.delete", { name });
      flash(`deleted workspace ${name}`);
    } catch (e) {
      flash(errorMessage(e) || `could not delete workspace ${name}`);
    }
  }

  async function openWorkspaceMenu(anchor: HTMLElement) {
    if (!connected()) return;
    try {
      setWorkspaces(await ws.command<string[]>("workspace.list"));
    } catch {
      // workspace list needs a connected msf database
    }
    const rect = anchor.getBoundingClientRect();
    const active = conn().workspace;
    const deletable = workspaces().filter(w => w !== "default" && w !== active);
    openContextMenu(rect.left, rect.bottom + 4, [
      { head: "Workspaces", sub: "switch, create, or delete msf workspace" },
      ...workspaces().map(w => ({
        label: w,
        icon: w === active ? "check" : undefined,
        fn: () => void switchWorkspace(w),
      })),
      { sep: true },
      { icon: "plus", label: "New workspace…", fn: () => setWorkspaceCreateOpen(true) },
      ...deletable.map(w => ({
        icon: "x",
        label: `Delete "${w}"`,
        danger: true,
        fn: () => void deleteWorkspace(w),
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
      const res = await ws.command<{ html?: string }>("report.html");
      if (typeof res?.html !== "string" || !res.html.trim()) {
        throw new Error("server returned an empty report");
      }
      const blob = new Blob([res.html], { type: "text/html;charset=utf-8" });
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
      <TopBar
        conn={conn}
        connected={connected}
        hostCount={() => hosts().length}
        serviceCount={() => services().length}
        liveCount={liveCount}
        credCount={() => creds().length}
        teamMode={teamMode}
        boundIdentity={boundIdentity}
        operatorName={operatorName}
        operators={operators}
        leftOpen={leftOpen}
        rightOpen={rightOpen}
        grid={grid}
        onDisconnect={disconnect}
        onExportReport={() => void exportReport()}
        onShowAbout={() => setShowAbout(true)}
        onShowKeys={() => setShowKeys(true)}
        onOpenScan={(mode) => setScan(mode)}
        onOpenBrute={() => setBruteOpen(true)}
        onOpenFind={() => setFindOpen(true)}
        onOpenHail={() => setHailOpen(true)}
        onOpenCmd={() => setCmdOpen(true)}
        onStage={(s) => { setNbMax(false); setStage(s); }}
        onTab={(t) => { setGraphFocus(false); setTab(t); }}
        onToggleLeft={() => setLeftOpen(v => !v)}
        onToggleRight={() => setRightOpen(v => !v)}
        onToggleGrid={() => setGrid(v => !v)}
        onSwitchOperator={() => {
          const previous = currentOperator();
          forgetOperator(localStorage);
          resetInteract();
          setOperatorName("");
          setOperatorDraft(previous);
        }}
        onOpenWorkspaces={(anchor) => void openWorkspaceMenu(anchor)}
      />

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
        <Inspector
          addr={selectedHost}
          onInteract={openInteract}
          onLogin={setLoginHost}
          onPivot={setPivotSid}
          onScan={(host) => { setScanTarget(host); setScan("services"); }}
          onFindAttacks={(host) => { selectHost(host); setFindOpen(true); }}
        />
      </aside>

      <NotebookDock
        tab={tab}
        onTab={setTab}
        liveCount={liveCount}
        jobCount={jobCount}
        runningOps={runningOps}
        maximized={() => nbMax() && !graphFocus()}
        onToggleMaximize={() => setNbMax(v => !v)}
        onInteract={openInteract}
        onScanSubnet={(subnet) => { setScanTarget(subnet); setScan("services"); }}
      />

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

      <AppModals
        conn={conn}
        connected={connected}
        sessions={sessions}
        selectedHost={selectedHost}
        scanTarget={scanTarget}
        cmdOpen={cmdOpen}
        hailOpen={hailOpen}
        bruteOpen={bruteOpen}
        findOpen={findOpen}
        loginHost={loginHost}
        pivotSid={pivotSid}
        scan={scan}
        launch={launch}
        workspaceCreateOpen={workspaceCreateOpen}
        showAbout={showAbout}
        showKeys={showKeys}
        teamMode={teamMode}
        boundIdentity={boundIdentity}
        operatorName={operatorName}
        operatorDraft={operatorDraft}
        confirmTeamDisconnect={confirmTeamDisconnect}
        wsStatus={wsStatus}
        protoMismatch={protoMismatch}
        serverVersion={serverVersion}
        onCloseCmd={() => setCmdOpen(false)}
        onPaletteAction={runPaletteAction}
        onSelectHostFromPalette={(addr) => { setNbMax(false); selectHost(addr); setStage("topo"); }}
        onInteract={openInteract}
        onLaunch={setLaunch}
        onCloseHail={() => setHailOpen(false)}
        onCloseBrute={() => setBruteOpen(false)}
        onCloseFind={() => setFindOpen(false)}
        onFindLaunch={(path, host, port) => {
          setFindOpen(false);
          selectHost(host);
          setLaunch({ type: "exploit", path, host, port });
        }}
        onCloseLogin={() => setLoginHost(undefined)}
        onClosePivot={() => setPivotSid(undefined)}
        onScanFromPivot={(subnet) => { setPivotSid(undefined); setScanTarget(subnet); setScan("services"); }}
        onCloseScan={() => { setScan(null); setScanTarget(undefined); }}
        onConfigureScan={(module, target) => {
          setScan(null);
          setLaunch({ type: "auxiliary", path: module, host: target });
        }}
        onCloseWorkspaceCreate={() => setWorkspaceCreateOpen(false)}
        onSwitchWorkspace={(name) => void switchWorkspace(name)}
        onCloseAbout={() => setShowAbout(false)}
        onCloseKeys={() => setShowKeys(false)}
        onOperatorDraft={setOperatorDraft}
        onCommitOperator={commitOperator}
        onCloseTeamDisconnect={() => setConfirmTeamDisconnect(false)}
        onConfirmTeamDisconnect={() => {
          setConfirmTeamDisconnect(false);
          void ws.command("disconnect").catch(() => {});
        }}
      />

      <Splitter area="lsp" onDelta={resizeLeft} onEnd={savePanels} />
      <Splitter area="rsp" onDelta={resizeRight} onEnd={savePanels} />
      <Splitter area="nsp" onDelta={resizeNotebook} onEnd={savePanels} />

      <ContextMenuRoot />
    </div>
  );
}
