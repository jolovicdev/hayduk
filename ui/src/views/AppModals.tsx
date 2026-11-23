import { Show } from "solid-js";
import type { ConnectionState, SessionState } from "../protocol/types";
import { CommandPalette } from "../components/CommandPalette";
import { Modal } from "../components/modal";
import { HaydukMark } from "../components/mark";
import ConnectDialog from "./ConnectDialog";
import { LaunchDialog } from "./LaunchDialog";
import { PivotDialog } from "./PivotDialog";
import { ScanDialog } from "./ScanDialog";
import { LoginDialog } from "./LoginDialog";
import { FindAttacksDialog } from "./FindAttacksDialog";
import { HailMaryDialog } from "./HailMaryDialog";
import { BruteDialog } from "./BruteDialog";
import { ws } from "../ws/singleton";

export type LaunchRequest = { type: string; path: string; host?: string; port?: number };

export function AppModals(props: {
  conn: () => ConnectionState;
  connected: () => boolean;
  sessions: () => Record<string, SessionState | undefined>;
  selectedHost: () => string | undefined;
  scanTarget: () => string | undefined;
  cmdOpen: () => boolean;
  hailOpen: () => boolean;
  bruteOpen: () => boolean;
  findOpen: () => boolean;
  loginHost: () => string | undefined;
  pivotSid: () => string | undefined;
  scan: () => "discovery" | "services" | null;
  launch: () => LaunchRequest | null;
  showAbout: () => boolean;
  showKeys: () => boolean;
  teamMode: () => boolean;
  boundIdentity: () => string;
  operatorName: () => string;
  operatorDraft: () => string;
  confirmTeamDisconnect: () => boolean;
  wsStatus: () => "connecting" | "open" | "closed";
  protoMismatch: () => boolean;
  serverVersion: () => string;
  onCloseCmd: () => void;
  onPaletteAction: (action: string) => void;
  onSelectHostFromPalette: (addr: string) => void;
  onInteract: (sid: string) => void;
  onLaunch: (req: LaunchRequest | null) => void;
  onCloseHail: () => void;
  onCloseBrute: () => void;
  onCloseFind: () => void;
  onFindLaunch: (path: string, host: string, port?: number) => void;
  onCloseLogin: () => void;
  onClosePivot: () => void;
  onScanFromPivot: (subnet: string) => void;
  onCloseScan: () => void;
  onConfigureScan: (module: string, target: string) => void;
  onCloseAbout: () => void;
  onCloseKeys: () => void;
  onOperatorDraft: (v: string) => void;
  onCommitOperator: () => void;
  onCloseTeamDisconnect: () => void;
  onConfirmTeamDisconnect: () => void;
}) {
  return (
    <>
      <Show when={props.cmdOpen()}>
        <CommandPalette
          connected={props.connected()}
          onClose={props.onCloseCmd}
          onAction={props.onPaletteAction}
          onSelectHost={props.onSelectHostFromPalette}
          onInteract={props.onInteract}
          onLaunchModule={(type, path) => props.onLaunch({ type, path, host: props.selectedHost() })}
        />
      </Show>

      <Show when={props.hailOpen()}>
        <HailMaryDialog host={props.selectedHost()} onClose={props.onCloseHail} />
      </Show>

      <Show when={props.bruteOpen()}>
        <BruteDialog host={props.selectedHost()} onClose={props.onCloseBrute} />
      </Show>

      <Show when={props.findOpen()}>
        <FindAttacksDialog host={props.selectedHost()}
          onLaunch={props.onFindLaunch}
          onClose={props.onCloseFind} />
      </Show>

      <Show when={props.loginHost()}>
        {(h) => <LoginDialog host={h()} onClose={props.onCloseLogin} />}
      </Show>

      <Show when={props.pivotSid()}>
        {(sid) => (
          <PivotDialog sid={sid()} meterpreter={props.sessions()[sid()]?.type === "meterpreter"}
            onScan={props.onScanFromPivot}
            onClose={props.onClosePivot} />
        )}
      </Show>

      <Show when={props.scan()}>
        {(mode) => (
          <ScanDialog mode={mode()} target={props.scanTarget() ?? props.selectedHost()}
            onConfigure={props.onConfigureScan}
            onClose={props.onCloseScan} />
        )}
      </Show>

      <Show when={props.launch()}>
        {(l) => <LaunchDialog type={l().type} path={l().path} prefillHost={l().host} prefillPort={l().port}
          onClose={() => props.onLaunch(null)} />}
      </Show>

      <Show when={props.conn().status === "disconnected" || props.conn().status === "connecting"}>
        <ConnectDialog conn={props.conn()} onConnect={(p) => ws.command("connect", p)} />
      </Show>

      <Show when={props.showAbout()}>
        <Modal title="About hayduk" onClose={props.onCloseAbout}>
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
            Hayduk {props.serverVersion() || ""} · jolovicdev · MIT
          </p>
          <div class="mbtns">
            <button class="abtn" onClick={props.onCloseAbout}>Close</button>
          </div>
        </Modal>
      </Show>

      <Show when={props.showKeys()}>
        <Modal title="Keyboard and mouse" onClose={props.onCloseKeys}>
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
            <button class="abtn" onClick={props.onCloseKeys}>Close</button>
          </div>
        </Modal>
      </Show>

      <Show when={props.teamMode() && !props.boundIdentity() && !props.operatorName()}>
        <Modal title="Who is operating?" onClose={() => props.onOperatorDraft("")}>
          <p class="mdesc">
            This Hayduk runs as a team server. Your name rides along on every command and lands
            next to your actions in the shared event log.
          </p>
          <div class="mform">
            <input value={props.operatorDraft()} placeholder="operator name" maxlength={32}
              onInput={(e) => props.onOperatorDraft(e.currentTarget.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && props.operatorDraft().trim()) props.onCommitOperator(); }}
              autocomplete="off" spellcheck={false} />
          </div>
          <div class="mbtns">
            <button class="abtn primary" disabled={!props.operatorDraft().trim()} onClick={props.onCommitOperator}>Join</button>
          </div>
        </Modal>
      </Show>

      <Show when={props.confirmTeamDisconnect()}>
        <Modal title="Disconnect the whole team?" onClose={props.onCloseTeamDisconnect}>
          <p class="mdesc">
            This Hayduk shares one msfrpcd link between all operators. Disconnecting drops
            it for everyone — live sessions, jobs, and consoles go with it. Campaign data
            and the event log stay.
          </p>
          <div class="mbtns">
            <button class="abtn" onClick={props.onCloseTeamDisconnect}>Keep connected</button>
            <button class="abtn red" onClick={props.onConfirmTeamDisconnect}>Disconnect team</button>
          </div>
        </Modal>
      </Show>

      <Show when={props.wsStatus() === "closed" && !props.protoMismatch()}>
        <div class="modalback show">
          <div class="modal" style="width:340px; text-align:center">
            <div class="mtitle" style="text-align:center">Connection to Hayduk lost</div>
            <p>Reconnecting automatically…</p>
          </div>
        </div>
      </Show>

      <Show when={props.protoMismatch()}>
        <div class="modalback show">
          <div class="modal" style="width:360px; text-align:center">
            <div class="mtitle" style="text-align:center">Incompatible Hayduk server</div>
            <p>This UI speaks protocol v1 but the server answered with a different version.
               Restart both from the same build.</p>
          </div>
        </div>
      </Show>
    </>
  );
}
