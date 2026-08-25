import { For, Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { campaignState } from "../stores/store";
import { flash } from "../statusflash";

export function HailMaryDialog(props: { host?: string; onClose: () => void }) {
  const hosts = () => campaignState().hosts.filter(h => !!h).map(h => h!.address);
  const initial = new Set<string>(hosts());
  if (props.host && hosts().includes(props.host)) {
    initial.clear();
    initial.add(props.host);
  }
  const [picked, setPicked] = createSignal<Set<string>>(initial);
  const [maxPerHost, setMaxPerHost] = createSignal("10");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const allPicked = () => picked().size === hosts().length;

  function toggle(addr: string) {
    setPicked(prev => {
      const next = new Set(prev);
      if (next.has(addr)) next.delete(addr);
      else next.add(addr);
      return next;
    });
  }

  function toggleAll() {
    setPicked(allPicked() ? new Set<string>() : new Set<string>(hosts()));
  }

  async function launch() {
    setBusy(true);
    setError("");
    try {
      const res = await ws.command<{ planned: number }>("campaign.hail_mary", {
        hosts: [...picked()],
        maxPerHost: Number(maxPerHost()) || 10,
      });
      props.onClose();
      flash(`hail mary under way: ${res.planned} launches planned; watch the event log`);
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Hail Mary" onClose={props.onClose} width="520px">
      <p style="margin-top:4px; font:400 12px/1.55 var(--sans); color:var(--tx2)">
        Fire every exploit the attack matcher offers at the chosen hosts, one after another, and
        see what lands. It is loud and intrusive by design. Matching is the same honest-and-dumb
        heuristic as Find attacks: module paths naming a service the host runs. Expect duds.
      </p>
      <div style="margin-top:14px; display:flex; align-items:center; gap:8px">
        <button class="abtn" style="flex:none" onClick={toggleAll}>
          {allPicked() ? "clear all" : "select all"}
        </button>
        <label style="display:grid; gap:4px; width:110px">
          <span style="font:500 11px var(--sans); color:var(--tx1)">Max per host</span>
          <input value={maxPerHost()} inputmode="numeric"
            onInput={(e) => setMaxPerHost(e.currentTarget.value)} autocomplete="off" />
        </label>
      </div>
      <div style="margin-top:10px; display:grid; gap:6px; max-height:240px; overflow:auto; padding-right:6px">
        <For each={hosts()}>{(addr) => (
          <label style="display:flex; align-items:center; gap:9px; background:var(--well);
            border:1px solid var(--line); border-radius:7px; padding:7px 10px; cursor:pointer">
            <input type="checkbox" checked={picked().has(addr)} onChange={() => toggle(addr)} />
            <span style="font:400 12px var(--mono); color:var(--tx0)">{addr}</span>
          </label>
        )}</For>
      </div>
      <Show when={error()}><p style="color:var(--red-br); margin-top:12px">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn" style="flex:none; padding:0 20px; background:var(--red); border-color:var(--red); color:#fff"
          disabled={busy() || picked().size === 0} onClick={() => void launch()}>
          {busy() ? "Launching…" : `Launch at ${picked().size} host${picked().size === 1 ? "" : "s"}`}
        </button>
      </div>
    </Modal>
  );
}
