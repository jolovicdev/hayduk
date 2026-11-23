import { For, Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import {
  autorouteAdd, autorouteAutoadd, autorouteRemove, isIPv4,
  parsePrefix, parseRouteTarget, prefixToNetmask, runAutoroute,
} from "./pivot";
import { parsePortForwards, type PortForwardRow } from "./sessionTools";
import { stopJob } from "./JobsView";
import { errorMessage } from "../errors";

export function PivotDialog(props: {
  sid: string; meterpreter: boolean;
  onClose: () => void;
  onScan?: (subnet: string) => void;
}) {
  const [section, setSection] = createSignal<"routes" | "socks" | "portfwd">("routes");
  const [mode, setMode] = createSignal<"auto" | "manual">(props.meterpreter ? "auto" : "manual");
  const [address, setAddress] = createSignal("");
  const [prefix, setPrefix] = createSignal("24");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [removing, setRemoving] = createSignal("");

  const ownRoutes = () =>
    campaignState().routes.filter(r => !!r && r.sessionId === props.sid);
  const socksJobs = () => Object.values(campaignState().jobs)
    .filter(j => !!j && j.name.includes("socks_proxy"));
  const [socksPort, setSocksPort] = createSignal("1080");
  const [forwards, setForwards] = createSignal<PortForwardRow[]>([]);
  const [fwdLocal, setFwdLocal] = createSignal("3389");
  const [fwdHost, setFwdHost] = createSignal("");
  const [fwdRemote, setFwdRemote] = createSignal("3389");

  async function runSession(command: string) {
    setError("");
    try {
      const res = await (await import("../ws/singleton")).ws
        .command<{ output: string }>("session.run", { sid: props.sid, command });
      return res.output;
    } catch (e) {
      setError(errorMessage(e));
      return "";
    }
  }

  async function listForwards() {
    const out = await runSession("portfwd list");
    if (out) setForwards(parsePortForwards(out));
  }

  async function startSocks() {
    setError("");
    try {
      await (await import("../ws/singleton")).ws.command("module.execute", {
        type: "auxiliary", name: "server/socks_proxy",
        options: { SRVHOST: "127.0.0.1", SRVPORT: Number(socksPort()) || 1080 },
      });
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  const prefixBits = () => parsePrefix(prefix());
  const valid = () =>
    mode() === "auto" ? props.meterpreter
      : isIPv4(address()) && prefixBits() !== undefined;

  function splitCIDR(value: string) {
    const cidr = /^(\d{1,3}(?:\.\d{1,3}){3})\/(\d{1,2})$/.exec(value.trim());
    if (cidr && isIPv4(cidr[1]!) && parsePrefix(cidr[2]!) !== undefined) {
      setAddress(cidr[1]!);
      setPrefix(cidr[2]!);
    }
  }

  async function removeRoute(subnet: string) {
    setRemoving(subnet);
    setError("");
    try {
      await runAutoroute(autorouteRemove(props.sid, parseRouteTarget(subnet)));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setRemoving("");
    }
  }

  async function pivot() {
    setBusy(true);
    setError("");
    try {
      await runAutoroute(
        mode() === "auto"
          ? autorouteAutoadd(props.sid)
          : autorouteAdd(props.sid, address().trim(), prefixBits()!),
      );
      props.onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Pivot via session ${props.sid}`} onClose={props.onClose} width="500px">
      <div class="seg" style="margin-top:12px">
        <button classList={{ on: section() === "routes" }} onClick={() => setSection("routes")}>
          <i aria-hidden="true" class="ph ph-signpost"></i>
          Routes{ownRoutes().length > 0 ? ` (${ownRoutes().length})` : ""}
        </button>
        <button classList={{ on: section() === "socks" }} onClick={() => setSection("socks")}>
          <i aria-hidden="true" class="ph ph-plug"></i>
          SOCKS{socksJobs().length > 0 ? ` (${socksJobs().length})` : ""}
        </button>
        <Show when={props.meterpreter}>
          <button classList={{ on: section() === "portfwd" }} onClick={() => setSection("portfwd")}>
            <i aria-hidden="true" class="ph ph-arrows-clockwise"></i>
            Port forwards
          </button>
        </Show>
      </div>

      <Show when={section() === "routes"}>
        <p class="mdesc">
          Route Metasploit traffic to an internal subnet through session {props.sid}. Added
          routes appear on the topology map as dashed pivot edges.
        </p>

        <Show when={ownRoutes().length > 0}>
          <div class="field" style="margin-top:12px; gap:6px">
            <b>Active routes on session {props.sid}</b>
            <For each={ownRoutes()}>{(r) => (
              <div class="fmatch">
                <div class="fminfo">
                  <span class="fmname">{r!.subnet}</span>
                  <span class="fmreason">routed through session {r!.sessionId}</span>
                </div>
                <Show when={props.onScan}>
                  <button class="abtn primary"
                    onClick={() => props.onScan!(r!.subnet)}>
                    <i aria-hidden="true" class="ph ph-wifi-high"></i>Scan
                  </button>
                </Show>
                <button class="abtn" disabled={removing() === r!.subnet}
                  onClick={() => void removeRoute(r!.subnet)}>
                  {removing() === r!.subnet ? "Removing…" : "Remove"}
                </button>
              </div>
            )}</For>
          </div>
        </Show>

        <div class="msec">
          <b>Add route</b>
          <div class="seg" style="justify-self:start">
            <button classList={{ on: mode() === "auto" }} disabled={!props.meterpreter}
              onClick={() => setMode("auto")}>
              Auto-detect
            </button>
            <button classList={{ on: mode() === "manual" }} onClick={() => setMode("manual")}>
              Manual subnet
            </button>
          </div>
          <Show when={!props.meterpreter}>
            <p class="mnote">
              Auto-detect requires Meterpreter; enter the target subnet manually for shell sessions.
            </p>
          </Show>
          <Show when={mode() === "manual"}>
            <div class="frow">
              <label class="field">
                <b>Subnet <span class="fdesc">(or paste CIDR)</span></b>
                <input value={address()} placeholder="10.24.16.0"
                  onInput={(e) => {
                    const value = e.currentTarget.value;
                    setAddress(value);
                    if (e.inputType === "insertFromPaste") splitCIDR(value);
                  }}
                  onBlur={(e) => splitCIDR(e.currentTarget.value)}
                  autocomplete="off" spellcheck={false} />
              </label>
              <label class="field">
                <b>Prefix</b>
                <input value={prefix()} inputmode="numeric" placeholder="24"
                  onInput={(e) => setPrefix(e.currentTarget.value)} autocomplete="off" />
              </label>
            </div>
            <Show when={address().trim() !== "" && !valid()}>
              <p class="mwarn" style="margin-top:4px">
                Enter an IPv4 network like 10.24.16.0 and prefix like 24 (pasting 10.24.16.0/24 splits automatically).
              </p>
            </Show>
            <Show when={valid() && address().trim() !== ""}>
              <p class="mnote" style="color:var(--tx1)">
                {address().trim()}/{prefixBits()} · netmask {prefixToNetmask(prefixBits()!)}
              </p>
            </Show>
          </Show>
        </div>
        <Show when={error()}><p class="merror">{error()}</p></Show>
        <div class="mbtns">
          <button class="abtn" onClick={props.onClose}>Close</button>
          <button class="abtn primary" disabled={busy() || !valid()}
            onClick={() => void pivot()}>
            {busy() ? "Routing…" : "Add pivot route"}
          </button>
        </div>
      </Show>

      <Show when={section() === "socks"}>
        <p class="mdesc">
          Starts a SOCKS proxy on the msfrpcd host forwarding through the workspace routing table
          (covers every active pivot route across sessions).
        </p>
        <div class="mform">
          <div style="display:flex; gap:8px; align-items:center">
            <span class="fdesc" style="font-family:var(--mono)">127.0.0.1:</span>
            <input value={socksPort()} inputmode="numeric" style="width:96px"
              onInput={(e) => setSocksPort(e.currentTarget.value)} aria-label="SOCKS port" />
            <button class="abtn primary" onClick={() => void startSocks()}>Start SOCKS</button>
          </div>
          <For each={socksJobs()}>{(j) => (
            <div class="tools-row">
              <span class="m">job {j!.id}</span><span>socks_proxy</span>
              <span class="dim">on msfrpcd host</span>
              <button class="abtn" onClick={() => stopJob(j!.id)}>Stop</button>
            </div>
          )}</For>
        </div>
        <Show when={error()}><p class="merror">{error()}</p></Show>
        <div class="mbtns">
          <button class="abtn" onClick={props.onClose}>Close</button>
        </div>
      </Show>

      <Show when={section() === "portfwd" && props.meterpreter}>
        <p class="mdesc">
          Forward a local port on the msfrpcd host to a host/port reachable from session {props.sid}.
        </p>
        <div class="mform">
          <div style="display:flex; gap:6px; align-items:center; flex-wrap:wrap">
            <input value={fwdLocal()} inputmode="numeric" style="width:78px" aria-label="Local port"
              onInput={(e) => setFwdLocal(e.currentTarget.value)} placeholder="local" />
            <input value={fwdHost()} style="flex:1; min-width:130px" aria-label="Remote host"
              onInput={(e) => setFwdHost(e.currentTarget.value)} placeholder="remote host" />
            <input value={fwdRemote()} inputmode="numeric" style="width:78px" aria-label="Remote port"
              onInput={(e) => setFwdRemote(e.currentTarget.value)} placeholder="remote" />
          </div>
          <div style="display:flex; gap:6px">
            <button class="abtn primary" disabled={!fwdHost().trim()} onClick={() => void runSession(
              `portfwd add -l ${Number(fwdLocal()) || 0} -p ${Number(fwdRemote()) || 0} -r ${fwdHost().trim()}`,
            ).then(listForwards)}>Add forward</button>
            <button class="abtn" onClick={() => void listForwards()}>Refresh list</button>
          </div>
          <For each={forwards()}>{(f) => (
            <div class="tools-row">
              <span class="m">{f.local}</span><span>→ {f.remote}</span>
              <button class="abtn" onClick={() => void runSession(
                `portfwd delete -l ${f.local.split(":").at(-1)}`).then(listForwards)}>Delete</button>
            </div>
          )}</For>
        </div>
        <Show when={error()}><p class="merror">{error()}</p></Show>
        <div class="mbtns">
          <button class="abtn" onClick={props.onClose}>Close</button>
        </div>
      </Show>
    </Modal>
  );
}
