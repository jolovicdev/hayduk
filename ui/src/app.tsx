import { For, Show, createSignal, onCleanup, onMount } from "solid-js";
import ConnectDialog from "./views/ConnectDialog";
import { ContextMenuRoot, closeContextMenu, openContextMenu } from "./components/contextmenu";
import { Dropdown, MenuItemButton } from "./components/dropdown";
import { Modal } from "./components/modal";
import { ModuleTree } from "./components/ModuleTree";
import { ConsoleView } from "./components/ConsoleView";
import { TopologyGraph } from "./components/TopologyGraph";
import { SessionsView } from "./views/SessionsView";
import { JobsView } from "./views/JobsView";
import { CredsView } from "./views/CredsView";
import { LootView } from "./views/LootView";
import { EventsView } from "./views/EventsView";
import { ServicesView } from "./views/ServicesView";
import { Inspector } from "./views/Inspector";
import { InteractView } from "./views/InteractView";
import { LaunchDialog } from "./views/LaunchDialog";
import { ScanDialog } from "./views/ScanDialog";
import { LoginDialog } from "./views/LoginDialog";
import { FindAttacksDialog } from "./views/FindAttacksDialog";
import { HailMaryDialog } from "./views/HailMaryDialog";
import { campaignState, protoMismatch, serverVersion, team as teamMode, wsStatus } from "./stores/store";
import { consoleBusy, consoleOutput, consolePrompt, tabs as consoleTabs, write } from "./stores/console";
import { attach } from "./stores/interact";
import { ws } from "./ws/singleton";
import { currentOperator } from "./ws/client";
import { forgetOperator, restoreOperator, saveOperator } from "./ws/operator";
import { flash } from "./statusflash";
import { HaydukMark } from "./components/mark";
import { DEFAULT_PANELS, clamp, clampNotebook, clampPanels, type PanelSizes } from "./stores/panels";

const notebookTabs: [string, string, string][] = [
  ["console", "terminal-window", "Console"],
  ["interact", "terminal", "Interact"],
  ["sessions", "broadcast", "Sessions"],
  ["jobs", "gear", "Jobs"],
  ["creds", "key", "Credentials"],
  ["loot", "archive", "Loot"],
  ["events", "pulse", "Events"],
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
  const [stage, setStage] = createSignal<"topo" | "svc">("topo");
  const [selectedHost, setSelectedHost] = createSignal<string | undefined>(undefined);
  const [launch, setLaunch] = createSignal<{ type: string; path: string; host?: string } | null>(null);
  const [scan, setScan] = createSignal<"discovery" | "services" | null>(null);
  const [loginHost, setLoginHost] = createSignal<string | undefined>(undefined);
  const [findOpen, setFindOpen] = createSignal(false);
  const [hailOpen, setHailOpen] = createSignal(false);
  const [showAbout, setShowAbout] = createSignal(false);
  const [showKeys, setShowKeys] = createSignal(false);
  const [grid, setGrid] = createSignal(true);
  const [tip, setTip] = createSignal(true);
  const [workspaces, setWorkspaces] = createSignal<string[]>([]);
  const [panels, setPanels] = createSignal(loadPanels());
  // a stored name must ride on commands again without re-prompting
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

  const conn = () => campaignState().connection;
  const liveCount = () => Object.keys(campaignState().sessions).length;
  const jobCount = () => Object.keys(campaignState().jobs).length;
  const totalModules = () => {
    const m = campaignState().modules;
    if (!m) return 0;
    return [m.exploits, m.auxiliary, m.post, m.payloads, m.encoders, m.nops, m.evasion]
      .reduce((a, v) => a + (v?.length ?? 0), 0);
  };

  function openInteract(sid: string) {
    void attach(sid);
    setTab("interact");
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

  // the list loads before the menu opens, or the menu renders empty and
  // whatever arrives later never shows
  async function openWorkspaceMenu(anchor: HTMLElement) {
    if (conn().status !== "connected") return;
    await loadWorkspaces();
    const rect = anchor.getBoundingClientRect();
    openContextMenu(rect.left, rect.bottom + 4, [
      { head: "Workspaces", sub: "switch the active msf workspace" },
      ...workspaces().map(w => ({
        label: w,
        icon: w === conn().workspace ? "check" : undefined,
        fn: () => void switchWorkspace(w),
      })),
    ]);
  }

  async function switchWorkspace(name: string) {
    try {
      await ws.command("workspace.set", { name });
      flash(`switched to workspace ${name}`);
    } catch (e: any) {
      flash(e.message ?? "workspace switch failed");
    }
  }

  function commitOperator() {
    const name = operatorDraft().trim();
    if (!name) return;
    saveOperator(name);
    setOperatorName(name);
  }

  function disconnect() {
    void ws.command("disconnect").catch(() => {});
  }

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
    } catch (e: any) {
      flash(e.message ?? "report export failed");
    }
  }

  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        closeContextMenu();
        setShowAbout(false);
        setShowKeys(false);
        return;
      }
      const tag = document.activeElement?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (e.key === "1") setStage("topo");
      else if (e.key === "2") setStage("svc");
      else if ((e.key === "f" || e.key === "F") && stage() === "topo") fit();
    };
    document.addEventListener("keydown", onKey);
    onCleanup(() => document.removeEventListener("keydown", onKey));
  });

  return (
    <div class="app" style={{
      "--w-left": `${panels().left}px`,
      "--w-right": `${panels().right}px`,
      "--h-nb": `${panels().nb}px`,
    }}>
      <header class="menubar card">
        <div class="brand">
          <HaydukMark size={21} />
          <span class="word">HAYDUK</span>
        </div>

        <Dropdown id="m-file" label="File">
          <MenuItemButton icon="plug" label="New connection…" onClick={() => disconnect()} />
          <MenuItemButton icon="x" label="Disconnect" disabled={conn().status === "disconnected"}
            onClick={() => disconnect()} />
          <div class="dsep"></div>
          <MenuItemButton icon="download-simple" label="Export report…" onClick={() => void exportReport()} />
          <MenuItemButton icon="info" label="About hayduk" onClick={() => setShowAbout(true)} />
        </Dropdown>

        <Dropdown id="m-camp" label="Campaign">
          <MenuItemButton icon="crosshair" label="Discover hosts…" disabled={conn().status !== "connected"}
            onClick={() => setScan("discovery")} />
          <MenuItemButton icon="wifi-high" label="Scan services…" disabled={conn().status !== "connected"}
            onClick={() => setScan("services")} />
          <MenuItemButton icon="key" label="Brute force logins…" disabled={conn().status !== "connected"}
            onClick={() => flash("right-click a host on the graph and pick Login as…")} />
          <div class="dsep"></div>
          <MenuItemButton icon="lightning" label="Find attacks…" disabled={conn().status !== "connected"}
            onClick={() => setFindOpen(true)} />
          <div class="dsep"></div>
          <MenuItemButton icon="fire" label="Hail Mary…" disabled={conn().status !== "connected"}
            onClick={() => setHailOpen(true)} />
        </Dropdown>

        <Dropdown id="m-view" label="View">
          <MenuItemButton icon="graph" label="Topology" hint="1" onClick={() => setStage("topo")} />
          <MenuItemButton icon="table" label="Services" hint="2" onClick={() => setStage("svc")} />
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

        <span class="spacer"></span>
        <Show when={teamMode()}>
          <span class="chip" title="Team session: click to change operator name"
            onClick={() => {
              const previous = currentOperator();
              forgetOperator(localStorage);
              setOperatorName("");
              setOperatorDraft(previous); // prefill the old name to edit
            }}>
            <i class="ph ph-users-three"></i>
            <b>{operatorName() || "unnamed"}</b> · {(campaignState().operators ?? []).length} live
          </span>
        </Show>
        <span class="chip" title="Active workspace: click to switch"
          onClick={(e) => void openWorkspaceMenu(e.currentTarget as HTMLElement)}>
          workspace <b>{conn().workspace || "-"}</b>
        </span>
      </header>

      <div class="toolbar card">
        <button class="tbtn" disabled={conn().status !== "connected"} title="Pick a module and run it"
          onClick={() => flash("right-click a module in the tree to launch it")}>
          <i class="ph ph-rocket-launch"></i>Launch attack
        </button>
        <button class="tbtn" disabled={liveCount() === 0} title="Open the sessions notebook"
          onClick={() => setTab("sessions")}>
          <i class="ph ph-broadcast"></i>Sessions
          <Show when={liveCount() > 0}><span class="badge">{liveCount()}</span></Show>
        </button>
        <span class="spacer"></span>
        <div class="sumchips" title="Campaign at a glance">
          <span class="schip"><i class="ph ph-crosshair"></i><b>{campaignState().hosts.length}</b> hosts</span>
          <span class="schip"><i class="ph ph-key"></i><b>{campaignState().creds.length}</b> creds</span>
          <span class="schip"><i class="ph ph-pulse"></i><b class="grn">{liveCount()}</b> live sessions</span>
        </div>
      </div>

      <aside class="left card">
        <div class="panelhead"><i class="ph ph-stack"></i><span class="pt">Modules</span>
          <span class="pc">{totalModules().toLocaleString()}</span>
        </div>
        <ModuleTree onLaunch={(type, path) => setLaunch({ type, path, host: selectedHost() })} />
      </aside>

      <main class="stage card">
        <div class="stagehead">
          <div class="seg">
            <button classList={{ on: stage() === "topo" }} onClick={() => setStage("topo")}>
              <i class="ph ph-graph"></i>Topology
            </button>
            <button classList={{ on: stage() === "svc" }} onClick={() => setStage("svc")}>
              <i class="ph ph-table"></i>Services
            </button>
          </div>
          <span class="spacer"></span>
          <Show when={stage() === "topo"}>
            <div class="zoomui">
              <button class="zbtn" aria-label="Zoom out" onClick={() => zoom(-1)}><i class="ph ph-minus"></i></button>
              <button class="zbtn" aria-label="Zoom in" onClick={() => zoom(1)}><i class="ph ph-plus"></i></button>
              <button class="zbtn" aria-label="Fit to view" onClick={fit} title="Fit graph to view (F)"><i class="ph ph-corners-out"></i></button>
            </div>
          </Show>
        </div>
        <div class="stagebody" classList={{ gridbg: stage() === "topo" && grid() }}>
          <div class="view" id="view-topo" hidden={stage() !== "topo"}>
            <TopologyGraph selected={selectedHost} onSelect={setSelectedHost}
              onInteract={openInteract}
              onLaunch={(host) => {
                setSelectedHost(host);
                flash(`host ${host} selected; right-click a module in the tree to launch against it`);
              }}
              onLogin={(host) => { setSelectedHost(host); setLoginHost(host); }} />
            <Show when={tip()}>
              <div class="tipbar" id="tipbar">
                <i class="ph ph-info info"></i>
                <span>Click a host to inspect it. <b>Right-click</b> for actions.</span>
                <button aria-label="Dismiss tip" onClick={() => setTip(false)}><i class="ph ph-x"></i></button>
              </div>
            </Show>
            <div class="legend">
              <span><i class="sw strip"></i>access obtained</span>
              <span><i class="sw dot"></i>live session</span>
              <span><i class="sw sq"></i>login possible</span>
              <span><i class="sw dash"></i>pivot route</span>
            </div>
          </div>
          <div class="view" hidden={stage() !== "svc"}>
            <ServicesView onInspect={(addr) => setSelectedHost(addr)} />
          </div>
        </div>
      </main>

      <aside class="right card">
        <div class="panelhead"><i class="ph ph-target"></i><span class="pt">Host details</span></div>
        <Inspector addr={selectedHost} onInteract={openInteract} onLogin={setLoginHost} />
      </aside>

      <section class="nb card">
        <div class="nbtabs">
          <For each={notebookTabs}>{([id, icon, label]) => (
            <button class="nbtab" classList={{ on: tab() === id }} onClick={() => setTab(id)}>
              <i class={`ph ph-${icon}`}></i>{label}
              <Show when={id === "sessions" && liveCount() > 0}>
                <span class="badge">{liveCount()}</span>
              </Show>
              <Show when={id === "jobs" && jobCount() > 0}>
                <span class="badge jobs">{jobCount()}</span>
              </Show>
            </button>
          )}</For>
        </div>
        <div class="nbbody">
          <div class="nbpane" hidden={tab() !== "console"}>
            <ConsoleView output={consoleOutput} prompt={consolePrompt()} busy={consoleBusy()}
              write={(cmd) => void write(cmd)}
              tabComplete={(line) => consoleTabs(line).catch(() => [])} />
          </div>
          <div class="nbpane" hidden={tab() !== "interact"}><InteractView /></div>
          <div class="nbpane" hidden={tab() !== "sessions"}>
            <SessionsView onInteract={openInteract} />
          </div>
          <div class="nbpane" hidden={tab() !== "jobs"}>
            <JobsView />
          </div>
          <div class="nbpane" hidden={tab() !== "creds"}><CredsView /></div>
          <div class="nbpane" hidden={tab() !== "loot"}><LootView /></div>
          <div class="nbpane" hidden={tab() !== "events"}><EventsView /></div>
        </div>
      </section>

      <footer class="status card">
        <i class={`ph ${conn().status === "connected" ? "ph-wifi-high wifi" : "ph-wifi-slash"}`}
           title="RPC link to the framework"></i>
        <span>
          {conn().status === "connected"
            ? `msfrpcd connected · metasploit ${conn().msfVersion}`
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

      <Show when={hailOpen()}>
        <HailMaryDialog host={selectedHost()} onClose={() => setHailOpen(false)} />
      </Show>

      <Show when={findOpen()}>
        <FindAttacksDialog host={selectedHost()}
          onLaunch={(path, host) => {
            setFindOpen(false);
            setSelectedHost(host);
            setLaunch({ type: "exploit", path, host });
          }}
          onClose={() => setFindOpen(false)} />
      </Show>

      <Show when={loginHost()}>
        {(h) => <LoginDialog host={h()} onClose={() => setLoginHost(undefined)} />}
      </Show>

      <Show when={scan()}>
        {(mode) => (
          <ScanDialog mode={mode()} target={selectedHost()}
            onConfigure={(module, target) => {
              setScan(null);
              setLaunch({ type: "auxiliary", path: module, host: target });
            }}
            onClose={() => setScan(null)} />
        )}
      </Show>

      <Show when={launch()}>
        {(l) => <LaunchDialog type={l().type} path={l().path} prefillHost={l().host} onClose={() => setLaunch(null)} />}
      </Show>

      <Show when={conn().status === "disconnected" || conn().status === "connecting"}>
        <ConnectDialog conn={conn()} onConnect={(p) => ws.command("connect", p)} />
      </Show>

      <Show when={showAbout()}>
        <Modal title="About Hayduk" onClose={() => setShowAbout(false)}>
          <div style="margin-top:10px; display:flex; align-items:center; gap:14px">
            <HaydukMark size={44} tile />
            <div>
              <p style="margin:0; font:600 10.5px var(--sans); letter-spacing:.13em; color:var(--red-br)">METASPLOIT OPERATIONS, MAPPED.</p>
              <p style="margin:2px 0 0; font:400 12px/1.55 var(--sans); color:var(--tx1)">
                Graphical attack management console for Metasploit: the lineage of Armitage,
                rebuilt as a single Go binary with a browser UI. For authorized security testing only.
              </p>
            </div>
          </div>
          <p style="margin:14px 0 0; font:400 11px var(--mono); color:var(--tx2)">
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
            <div class="krow"><span class="keys"><kbd>1</kbd><kbd>2</kbd></span>Switch between Topology and Services</div>
            <div class="krow"><span class="keys"><kbd>F</kbd></span>Fit the graph to the view</div>
            <div class="krow"><span class="keys"><kbd>Esc</kbd></span>Close menus and dialogs</div>
            <div class="krow"><span class="keys"><kbd>R-click</kbd></span>Context actions on hosts, modules and table rows</div>
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
          <p style="margin-top:4px; font:400 12px/1.55 var(--sans); color:var(--tx2)">
            This Hayduk runs as a team server. Your name rides along on every command and lands
            next to your actions in the shared event log.
          </p>
          <input style="margin-top:14px" value={operatorDraft()} placeholder="operator name"
            onInput={(e) => setOperatorDraft(e.currentTarget.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && operatorDraft().trim()) commitOperator(); }}
            autocomplete="off" spellcheck={false} />
          <div class="mbtns">
            <button class="abtn" style="flex:none; padding:0 20px"
              disabled={!operatorDraft().trim()} onClick={commitOperator}>Join</button>
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

      <div class="narrownote">
        <div>
          <b>Hayduk needs a desktop window.</b><br />
          This is a dense operator console. Open it in a viewport<br />
          wider than 960px, ideally 1280px or more.
        </div>
      </div>
    </div>
  );
}
