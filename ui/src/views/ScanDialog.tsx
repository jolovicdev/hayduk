import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { DISCOVERY_MODULES, SERVICE_MODULES, pickModules } from "./scan";

export function ScanDialog(props: {
  mode: "discovery" | "services";
  target?: string;
  onConfigure: (module: string, target: string) => void;
  onClose: () => void;
}) {
  const discovery = () => props.mode === "discovery";
  const modules = createMemo(() =>
    pickModules(campaignState().modules?.auxiliary, discovery() ? DISCOVERY_MODULES : SERVICE_MODULES));
  const [target, setTarget] = createSignal(props.target ?? "");
  const [mod, setMod] = createSignal("");
  const chosen = () => mod() || modules()[0] || "";
  const valid = () => target().trim() !== "" && chosen() !== "";

  return (
    <Modal title={discovery() ? "Discover hosts" : "Scan services"} onClose={props.onClose} width="480px">
      <p style="margin-top:4px; font:400 12px/1.55 var(--sans); color:var(--tx2)">
        {discovery()
          ? "Sweep a range for live hosts. Anything that answers lands in the workspace database and on the graph."
          : "Port-scan a host or range. Discovered services fill the services table and the host inspector."}
      </p>
      <label style="margin-top:14px; display:grid; gap:4px">
        <span style="font:500 11px var(--sans); color:var(--tx1)">
          Target <span style="color:var(--tx2)">(a host or CIDR range, sent as RHOSTS)</span>
        </span>
        <input value={target()} placeholder="10.0.0.0/24"
          onInput={(e) => setTarget(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
      </label>
      <label style="margin-top:10px; display:grid; gap:4px">
        <span style="font:500 11px var(--sans); color:var(--tx1)">Scanner module</span>
        <select value={chosen()} onChange={(e) => setMod(e.currentTarget.value)}>
          <For each={modules()}>{(m) => <option value={m}>{m}</option>}</For>
        </select>
      </label>
      <Show when={modules().length === 0}>
        <p style="color:var(--red-br); margin-top:10px">
          No scanner modules from this build's catalogue; pick one from the module tree instead.
        </p>
      </Show>
      <div class="mbtns">
        <button class="abtn" style="flex:none; padding:0 20px" disabled={!valid()}
          onClick={() => props.onConfigure(chosen(), target().trim())}>
          Configure…
        </button>
      </div>
    </Modal>
  );
}
