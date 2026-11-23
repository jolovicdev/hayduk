import { Show } from "solid-js";
import type { ConnectionState } from "../protocol/types";
import { Dropdown, MenuItemButton } from "./dropdown";
import { HaydukMark } from "./mark";

export function TopBar(props: {
  conn: () => ConnectionState;
  connected: () => boolean;
  hostCount: () => number;
  serviceCount: () => number;
  liveCount: () => number;
  credCount: () => number;
  teamMode: () => boolean;
  boundIdentity: () => string;
  operatorName: () => string;
  operators: () => string[];
  leftOpen: () => boolean;
  rightOpen: () => boolean;
  grid: () => boolean;
  onDisconnect: () => void;
  onExportReport: () => void;
  onShowAbout: () => void;
  onShowKeys: () => void;
  onOpenScan: (mode: "discovery" | "services") => void;
  onOpenBrute: () => void;
  onOpenFind: () => void;
  onOpenHail: () => void;
  onOpenCmd: () => void;
  onStage: (s: "topo" | "svc" | "hosts") => void;
  onTab: (t: string) => void;
  onToggleLeft: () => void;
  onToggleRight: () => void;
  onToggleGrid: () => void;
  onSwitchOperator: () => void;
  onOpenWorkspaces: (anchor: HTMLElement) => void;
}) {
  const effectiveOperator = () => props.boundIdentity() || props.operatorName() || "unnamed";

  return (
    <header class="menubar">
      <div class="brand">
        <HaydukMark size={18} />
        <span class="word">hayduk</span>
      </div>

      <nav class="mnav" aria-label="Main menu">
        <Dropdown id="m-file" label="File">
          <MenuItemButton icon="x" label="Disconnect" disabled={props.conn().status === "disconnected"}
            onClick={props.onDisconnect} />
          <div class="dsep"></div>
          <MenuItemButton icon="download-simple" label="Export report…" disabled={!props.connected()} onClick={props.onExportReport} />
          <MenuItemButton icon="info" label="About hayduk" onClick={props.onShowAbout} />
        </Dropdown>

        <Dropdown id="m-camp" label="Campaign">
          <MenuItemButton icon="crosshair" label="Discover hosts…" disabled={!props.connected()}
            onClick={() => props.onOpenScan("discovery")} />
          <MenuItemButton icon="wifi-high" label="Scan services…" disabled={!props.connected()}
            onClick={() => props.onOpenScan("services")} />
          <MenuItemButton icon="key" label="Brute force logins…" disabled={!props.connected()}
            onClick={props.onOpenBrute} />
          <div class="dsep"></div>
          <MenuItemButton icon="lightning" label="Find attacks…" disabled={!props.connected()}
            onClick={props.onOpenFind} />
          <div class="dsep"></div>
          <MenuItemButton icon="fire" label="Hail Mary…" disabled={!props.connected()}
            onClick={props.onOpenHail} />
        </Dropdown>

        <Dropdown id="m-view" label="View">
          <MenuItemButton icon="graph" label="Topology" hint="1" onClick={() => props.onStage("topo")} />
          <MenuItemButton icon="table" label="Services" hint="2" onClick={() => props.onStage("svc")} />
          <MenuItemButton icon="stack" label="Hosts" hint="3" onClick={() => props.onStage("hosts")} />
          <div class="dsep"></div>
          <MenuItemButton icon="stack" label={props.leftOpen() ? "Hide modules" : "Show modules"} hint="["
            onClick={props.onToggleLeft} />
          <MenuItemButton icon="target" label={props.rightOpen() ? "Hide inspector" : "Show inspector"} hint="]"
            onClick={props.onToggleRight} />
          <MenuItemButton icon="command" label="Command palette" hint="Ctrl+K"
            onClick={props.onOpenCmd} />
          <div class="dsep"></div>
          <MenuItemButton icon="dots-nine" label={props.grid() ? "Dot grid: on" : "Dot grid: off"}
            onClick={props.onToggleGrid} />
          <MenuItemButton icon="arrows-out-simple" label="Fullscreen" hint="F11"
            onClick={() => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else void document.documentElement.requestFullscreen();
            }} />
        </Dropdown>

        <Dropdown id="m-help" label="Help">
          <MenuItemButton icon="keyboard" label="Keyboard and mouse" onClick={props.onShowKeys} />
          <div class="dsep"></div>
          <MenuItemButton icon="info" label="About hayduk" onClick={props.onShowAbout} />
        </Dropdown>
      </nav>

      <span class="spacer"></span>

      <div class="telemetry" aria-label="Campaign telemetry">
        <button class="tstat" onClick={() => props.onStage("topo")} title="View hosts on topology">
          HOSTS <b>{props.hostCount()}</b>
        </button>
        <button class="tstat" onClick={() => props.onStage("svc")} title="View discovered services">
          SVCS <b>{props.serviceCount()}</b>
        </button>
        <button class="tstat" classList={{ live: props.liveCount() > 0 }} onClick={() => props.onTab("sessions")} title="View live sessions">
          <span class="tdot"></span>SESSIONS <b>{props.liveCount()}</b>
        </button>
        <button class="tstat" onClick={() => props.onTab("creds")} title="View recovered credentials">
          CREDS <b>{props.credCount()}</b>
        </button>
      </div>

      <button class="cmd-trigger" onClick={props.onOpenCmd} title="Open command palette (Ctrl+K)">
        <i aria-hidden="true" class="ph ph-magnifying-glass"></i>
        <span>Command</span>
        <kbd>Ctrl+K</kbd>
      </button>

      <div class="bar-actions">
        <button class="tbtn" disabled={!props.connected()} onClick={props.onExportReport} title="Export HTML campaign report">
          <i aria-hidden="true" class="ph ph-download-simple"></i>Export
        </button>
        <button class="tbtn" disabled={!props.connected()} onClick={() => props.onOpenScan("services")} title="Scan target services">
          <i aria-hidden="true" class="ph ph-wifi-high"></i>Scan
        </button>
        <button class="tbtn primary" disabled={!props.connected()} onClick={() => props.onOpenScan("discovery")} title="Discover hosts on target network">
          <i aria-hidden="true" class="ph ph-plus"></i>Discover
        </button>
      </div>

      <Show when={props.teamMode()}>
        <span
          class="chip team-chip"
          style={props.boundIdentity() ? "cursor:default" : undefined}
          title={props.boundIdentity()
            ? `Bound operator token (${effectiveOperator()}). Online: ${props.operators().join(", ") || "nobody"}`
            : `Team session: click to change operator name. Online: ${props.operators().join(", ") || "nobody"}`}
          onClick={() => {
            if (!props.boundIdentity()) props.onSwitchOperator();
          }}
        >
          <i aria-hidden="true" class="ph ph-users-three"></i>
          <b>{effectiveOperator()}</b> · {props.operators().length}
        </span>
      </Show>

      <button class="chip workspace-chip" disabled={!props.connected()} title="Active workspace: click to switch"
        onClick={(e) => props.onOpenWorkspaces(e.currentTarget as HTMLElement)}>
        <i aria-hidden="true" class="ph ph-stack"></i>
        <b>{props.conn().workspace || "offline"}</b>
        <i aria-hidden="true" class="ph ph-caret-down"></i>
      </button>
    </header>
  );
}
