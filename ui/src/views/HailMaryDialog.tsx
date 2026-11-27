import { For, Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { ws } from "../ws/singleton";
import { campaignState } from "../stores/store";
import { flash } from "../statusflash";
import { errorMessage } from "../errors";

export function HailMaryDialog(props: { host?: string; hosts?: string[]; onClose: () => void }) {
  const hosts = () => campaignState().hosts.filter(h => !!h).map(h => h!.address);
  const initial = new Set<string>(hosts());
  const preselected = (props.hosts && props.hosts.length > 0 ? props.hosts : (props.host ? [props.host] : []))
    .filter(h => hosts().includes(h));
  if (preselected.length > 0) {
    initial.clear();
    for (const addr of preselected) initial.add(addr);
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
      flash(`hail mary under way: ${res.planned} launches planned — follow or cancel it in Operations`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Hail Mary" onClose={props.onClose} width="520px">
      <p class="mdesc">
        Run every matching exploit against the selected hosts in sequence. This can disrupt
        target services. Matches use the same service names and OS family as Find attacks;
        versions and patch levels are not checked.
      </p>
      <div style="margin-top:14px; display:flex; align-items:center; gap:8px">
        <button class="abtn" onClick={toggleAll}>
          {picked().size > 0 && allPicked() ? "clear all" : "select all"}
        </button>
        <label class="field" style="width:110px">
          <b>Max per host</b>
          <input value={maxPerHost()} inputmode="numeric"
            onInput={(e) => setMaxPerHost(e.currentTarget.value)} autocomplete="off" />
        </label>
      </div>
      <div class="picklist">
        <For each={hosts()}>{(addr) => (
          <label class="pickrow">
            <input type="checkbox" checked={picked().has(addr)} onChange={() => toggle(addr)} />
            <span class="addr">{addr}</span>
          </label>
        )}</For>
      </div>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn red" disabled={busy() || picked().size === 0} onClick={() => void launch()}>
          {busy() ? "Launching…" : `Launch at ${picked().size} host${picked().size === 1 ? "" : "s"}`}
        </button>
      </div>
    </Modal>
  );
}
