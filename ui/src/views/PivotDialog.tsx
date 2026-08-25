import { Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { CommandError } from "../ws/client";
import { autorouteAdd, autorouteAutoadd, isIPv4, prefixToNetmask, runAutoroute } from "./pivot";

export function PivotDialog(props: { sid: string; meterpreter: boolean; onClose: () => void }) {
  const [mode, setMode] = createSignal<"auto" | "manual">(props.meterpreter ? "auto" : "manual");
  const [address, setAddress] = createSignal("");
  const [prefix, setPrefix] = createSignal("24");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const valid = () =>
    mode() === "auto" ? props.meterpreter
      : isIPv4(address()) && prefixToNetmask(Number(prefix())) !== undefined;

  async function pivot() {
    setBusy(true);
    setError("");
    try {
      await runAutoroute(
        mode() === "auto"
          ? autorouteAutoadd(props.sid)
          : autorouteAdd(props.sid, address().trim(), Number(prefix())),
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
      <p style="margin-top:4px; font:400 12px/1.55 var(--sans); color:var(--tx2)">
        Route traffic to another network through this session. The new pivot appears on the
        topology as a dashed route edge.
      </p>
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
        <p style="font:400 11px/1.5 var(--sans); color:var(--tx2); margin-top:8px">
          Auto-detect needs a meterpreter session; enter the subnet by hand instead.
        </p>
      </Show>
      <Show when={mode() === "manual"}>
        <div style="margin-top:12px; display:grid; grid-template-columns:1.6fr 1fr; gap:10px">
          <label style="display:grid; gap:4px">
            <span style="font:500 11px var(--sans); color:var(--tx1)">Subnet</span>
            <input value={address()} placeholder="10.13.37.0"
              onInput={(e) => setAddress(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
          </label>
          <label style="display:grid; gap:4px">
            <span style="font:500 11px var(--sans); color:var(--tx1)">Prefix</span>
            <input value={prefix()} inputmode="numeric" placeholder="24"
              onInput={(e) => setPrefix(e.currentTarget.value)} autocomplete="off" />
          </label>
        </div>
      </Show>
      <Show when={error()}><p style="color:var(--red-br); margin-top:12px">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn" style="flex:none; padding:0 20px" disabled={busy() || !valid()}
          onClick={() => void pivot()}>
          {busy() ? "Routing…" : "Add pivot route"}
        </button>
      </div>
    </Modal>
  );
}
