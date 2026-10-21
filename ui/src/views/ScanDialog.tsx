import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { flash } from "../statusflash";
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
  // Version detection is the default for a services scan: it names the
  // service and imports the version string, so the table and the attack
  // matcher are useful without a console detour.
  const [version, setVersion] = createSignal(true);
  const [ports, setPorts] = createSignal("");
  const [skipDiscovery, setSkipDiscovery] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const chosen = () => mod() || modules()[0] || "";
  const versionScan = () => !discovery() && version();
  const valid = () => target().trim() !== "" && (versionScan() || chosen() !== "");

  async function start() {
    if (!versionScan()) {
      props.onConfigure(chosen(), target().trim());
      return;
    }
    setBusy(true);
    setError("");
    try {
      await ws.command("services.scan", {
        target: target().trim(),
        ports: ports().trim(),
        skipDiscovery: skipDiscovery(),
      });
      flash("service scan started - results land in Services and Events");
      props.onClose();
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
      setBusy(false);
    }
  }

  return (
    <Modal title={discovery() ? "Discover hosts" : "Scan services"} onClose={props.onClose} width="520px">
      <p class="mdesc">
        {discovery()
          ? "Sweep a range for live hosts. Anything that answers lands in the workspace database and on the graph."
          : versionScan()
            ? "Fingerprint a host or range with nmap -sV. Names, versions, and open ports are imported into the workspace."
            : "Port-scan a host or range. Discovered services fill the services table and the host inspector."}
      </p>
      <div class="mform">
        <label class="field">
          <b>Target <span class="fdesc">(a host, CIDR range, or several space-separated targets)</span></b>
          <input value={target()} placeholder="10.0.0.0/24"
            onInput={(e) => setTarget(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
        </label>

        <Show when={!discovery()}>
          <label class="checkline">
            <input type="checkbox" checked={version()} onChange={(e) => setVersion(e.currentTarget.checked)} />
            detect service versions (nmap -sV)
          </label>
        </Show>

        <Show when={versionScan()}>
          <label class="field">
            <b>Ports <span class="fdesc">(blank: nmap's top 1000 TCP ports)</span></b>
            <input value={ports()} placeholder="top 1000"
              onInput={(e) => setPorts(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
          </label>
          <label class="checkline">
            <input type="checkbox" checked={skipDiscovery()} onChange={(e) => setSkipDiscovery(e.currentTarget.checked)} />
            skip host discovery (-Pn); use when the target blocks ping
          </label>
          <p class="mnote">
            Runs through Metasploit's db_nmap on the framework host. A range can take several minutes;
            progress and the result appear in Events.
          </p>
        </Show>

        <Show when={!versionScan()}>
          <label class="field">
            <b>Scanner module</b>
            <select value={chosen()} onChange={(e) => setMod(e.currentTarget.value)}>
              <For each={modules()}>{(m) => <option value={m}>{m}</option>}</For>
            </select>
          </label>
        </Show>
      </div>
      <Show when={!versionScan() && modules().length === 0}>
        <p class="merror">
          No scanner modules from this build's catalogue; pick one from the module tree instead.
        </p>
      </Show>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn" disabled={!valid() || busy()} onClick={() => void start()}>
          {busy() ? "Starting…" : versionScan() ? "Start version scan" : "Launch…"}
        </button>
      </div>
    </Modal>
  );
}
