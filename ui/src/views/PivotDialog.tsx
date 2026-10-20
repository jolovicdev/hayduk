import { For, Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { CommandError } from "../ws/client";
import {
  autorouteAdd, autorouteAutoadd, autorouteRemove, isIPv4,
  parsePrefix, parseRouteTarget, prefixToNetmask, runAutoroute,
} from "./pivot";
import { parsePortForwards, type PortForwardRow } from "./sessionTools";
import { stopJob } from "./JobsView";

export function PivotDialog(props: {
  sid: string; meterpreter: boolean;
  onClose: () => void;
  onScan?: (subnet: string) => void;
}) {
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
      setError(e instanceof CommandError ? e.message : String(e));
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
      setError(e instanceof CommandError ? e.message : String(e));
    }
  }

  const prefixBits = () => parsePrefix(prefix());
  const valid = () =>
    mode() === "auto" ? props.meterpreter
      : isIPv4(address()) && prefixBits() !== undefined;

  // Split on paste or blur so a partially typed prefix stays in the input.
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
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
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
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Pivot session ${props.sid}`} onClose={props.onClose} width="460px">
      <p class="mdesc">
        Route traffic to another network through this session. The new pivot appears on the
        topology as a dashed route edge.
      </p>
      <Show when={ownRoutes().length > 0}>
        <div class="field" style="margin-top:14px; gap:6px">
          <b>Routes via this session</b>
          <For each={ownRoutes()}>{(r) => (
            <div class="fmatch">
              <div class="fminfo">
                <span class="fmname">{r!.subnet}</span>
                <span class="fmreason">via session {r!.sessionId}</span>
              </div>
              <Show when={props.onScan}>
                <button class="abtn"
                  onClick={() => props.onScan!(r!.subnet)}>Scan</button>
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
        <b>SOCKS proxy</b>
        <p class="mnote">
          Listens on the msfrpcd host and forwards through the workspace routing
          table, so it reaches the networks every pivot above has added - this
          session's included. It is workspace-wide, not scoped to this session.
        </p>
        <div style="display:flex; gap:8px; align-items:center">
          <span class="fdesc">127.0.0.1:</span>
          <input value={socksPort()} inputmode="numeric" style="width:90px"
            onInput={(e) => setSocksPort(e.currentTarget.value)} aria-label="SOCKS port" />
          <button class="abtn" onClick={() => void startSocks()}>Start SOCKS</button>
        </div>
        <For each={socksJobs()}>{(j) => (
          <div class="tools-row">
            <span class="m">job {j!.id}</span><span>socks_proxy</span>
            <span class="dim">on the msfrpcd host</span>
            <button class="abtn" onClick={() => stopJob(j!.id)}>Stop</button>
          </div>
        )}</For>
      </div>
      <Show when={props.meterpreter}>
        <div class="msec">
          <b>Port forwards</b>
          <div style="display:flex; gap:8px; align-items:center; flex-wrap:wrap">
            <input value={fwdLocal()} inputmode="numeric" style="width:80px" aria-label="Local port"
              onInput={(e) => setFwdLocal(e.currentTarget.value)} placeholder="local" />
            <input value={fwdHost()} style="width:130px" aria-label="Remote host"
              onInput={(e) => setFwdHost(e.currentTarget.value)} placeholder="remote host" />
            <input value={fwdRemote()} inputmode="numeric" style="width:80px" aria-label="Remote port"
              onInput={(e) => setFwdRemote(e.currentTarget.value)} placeholder="remote" />
            <button class="abtn" onClick={() => void runSession(
              `portfwd add -l ${Number(fwdLocal()) || 0} -p ${Number(fwdRemote()) || 0} -r ${fwdHost().trim()}`,
            ).then(listForwards)}>Add forward</button>
            <button class="abtn" onClick={() => void listForwards()}>List</button>
          </div>
          <For each={forwards()}>{(f) => (
            <div class="tools-row">
              <span class="m">{f.local}</span><span>→ {f.remote}</span>
              <button class="abtn" onClick={() => void runSession(
                `portfwd delete -l ${f.local.split(":").at(-1)}`).then(listForwards)}>Delete</button>
            </div>
          )}</For>
        </div>
      </Show>
      <div class="seg" style="margin-top:14px; display:inline-flex">
        <button classList={{ on: mode() === "auto" }} disabled={!props.meterpreter}
          onClick={() => setMode("auto")}>
          Auto-detect
        </button>
        <button classList={{ on: mode() === "manual" }} onClick={() => setMode("manual")}>
          Manual subnet
        </button>
      </div>
      <Show when={!props.meterpreter}>
        <p class="mdesc" style="margin-top:8px">
          Auto-detect needs a meterpreter session; enter the subnet by hand instead.
        </p>
      </Show>
      <Show when={mode() === "manual"}>
        <div class="frow" style="margin-top:12px">
          <label class="field">
            <b>Subnet</b>
            <input value={address()} placeholder="10.13.37.0"
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
          <p class="mwarn" style="margin-top:10px">
            Enter a bare IPv4 address like 10.13.37.0 and a prefix like 24 - pasting
            10.13.37.0/24 splits across both fields automatically.
          </p>
        </Show>
        <Show when={valid() && address().trim() !== ""}>
          <p class="mnote" style="margin-top:10px; color:var(--tx1)">
            {address().trim()}/{prefixBits()} · {prefixToNetmask(prefixBits()!)}
          </p>
        </Show>
      </Show>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn" disabled={busy() || !valid()}
          onClick={() => void pivot()}>
          {busy() ? "Routing…" : "Add pivot route"}
        </button>
      </div>
    </Modal>
  );
}
