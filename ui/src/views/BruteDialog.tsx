import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import {
  BRUTE_CAP, LOGIN_SCANNERS, buildBrutePlan, defaultScannerForHost, parseLines,
  servicePortForHost,
} from "./brute";
import { errorMessage } from "../errors";

const DEFAULT_USERS = "Administrator\nadmin\nuser";
const DEFAULT_PASSES = "password\n123456\nadmin\nuser\ntest";

export function BruteDialog(props: { host?: string; onClose: () => void }) {
  const [hosts, setHosts] = createSignal(props.host ?? "");
  const [service, setService] = createSignal(
    defaultScannerForHost(props.host, campaignState().services)?.service ?? LOGIN_SCANNERS[0]!.service,
  );
  const [manualPort, setManualPort] = createSignal("");
  const [users, setUsers] = createSignal(DEFAULT_USERS);
  const [passes, setPasses] = createSignal(DEFAULT_PASSES);
  const [addBlank, setAddBlank] = createSignal(true);
  const [addUserAsPass, setAddUserAsPass] = createSignal(false);
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const scanner = createMemo(() =>
    LOGIN_SCANNERS.find(s => s.service === service()) ?? LOGIN_SCANNERS[0]!);
  const hostList = createMemo(() => parseLines(hosts()));
  const userList = createMemo(() => (scanner().userless ? [] : parseLines(users())));
  const passList = createMemo(() => parseLines(passes()));
  // A login scanner defaults to its well-known port. The scan already found
  // the service somewhere else, so carry that port into every attempt.
  const scannedPorts = createMemo(() => {
    const out: Record<string, number> = {};
    for (const host of hostList()) {
      const port = servicePortForHost(host, scanner(), campaignState().services);
      if (port) out[host] = port;
    }
    return out;
  });
  const portOverride = () => {
    const text = manualPort().trim();
    if (!text) return undefined;
    const port = Number(text);
    return Number.isInteger(port) && port > 0 && port <= 65535 ? port : undefined;
  };
  const portInvalid = () => manualPort().trim() !== "" && portOverride() === undefined;

  // Preview only: the engine builds the authoritative plan and applies the
  // same cap, so a run cannot spray more than the dialog promised.
  const plan = createMemo(() => buildBrutePlan({
    hosts: hostList(), users: userList(), passes: passList(),
    module: scanner().module, userless: scanner().userless,
    addBlank: addBlank(), addUserAsPass: addUserAsPass(),
    port: portOverride(), ports: scannedPorts(),
    cap: BRUTE_CAP,
  }));
  const valid = () => hostList().length > 0 && passList().length + (addBlank() ? 1 : 0) > 0
    && (scanner().userless || userList().length > 0) && !portInvalid();

  async function start() {
    const p = plan();
    if (p.attempts.length === 0 || portInvalid()) return;
    setBusy(true);
    setError("");
    try {
      const res = await ws.command<{ operationId: string; planned: number; capped: number }>(
        "brute.start",
        {
          hosts: hostList(),
          module: scanner().module,
          users: userList(),
          passwords: passList(),
          ports: scannedPorts(),
          port: portOverride() ?? 0,
          addBlank: addBlank(),
          addUserAsPass: addUserAsPass(),
          userless: !!scanner().userless,
        },
      );
      props.onClose();
      const capped = res.capped > 0 ? `; ${res.capped} beyond the cap skipped` : "";
      flash(`brute force started: ${res.planned} attempt(s)${capped} — follow or cancel it in Operations`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Brute force logins" onClose={props.onClose} width="560px">
      <p class="mdesc">
        Sprays credential pairs at a login service and stores anything that works in the
        workspace. The run continues if you close this dialog; watch it in Operations.
        Verify authorization before pointing this at a host.
      </p>
      <div class="mform">
        <label class="field">
          <b>Hosts <span class="fdesc">(one per line)</span></b>
          <textarea rows={3} value={hosts()} spellcheck={false}
            placeholder="10.0.0.5" onInput={(e) => setHosts(e.currentTarget.value)} />
        </label>
        <div class="frow even">
          <label class="field">
            <b>Service</b>
            <select value={service()} onChange={(e) => setService(e.currentTarget.value)}>
              <For each={LOGIN_SCANNERS}>{(s) => <option value={s.service}>{s.label}</option>}</For>
            </select>
          </label>
          <label class="field">
            <b>Port <span class="fdesc">(blank: use the scanned port)</span></b>
            <input value={manualPort()} inputMode="numeric" placeholder="auto"
              onInput={(e) => setManualPort(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
          </label>
        </div>
        <Show when={portInvalid()}>
          <p class="merror">Port must be between 1 and 65535.</p>
        </Show>
        <Show when={!manualPort().trim() && Object.keys(scannedPorts()).length > 0}>
          <p class="mnote">
            Using scanned port{Object.keys(scannedPorts()).length === 1 ? "" : "s"}:{" "}
            {Object.entries(scannedPorts()).map(([host, port]) => `${host}:${port}`).join(" · ")}
          </p>
        </Show>
        <div class="frow even">
          <label class="field">
            <b>Users <span class="fdesc">(one per line)</span></b>
            <textarea rows={5} value={users()} spellcheck={false} disabled={scanner().userless}
              onInput={(e) => setUsers(e.currentTarget.value)} />
          </label>
          <label class="field">
            <b>Passwords <span class="fdesc">(one per line)</span></b>
            <textarea rows={5} value={passes()} spellcheck={false}
              onInput={(e) => setPasses(e.currentTarget.value)} />
          </label>
        </div>
        <label class="checkline">
          <input type="checkbox" checked={addBlank()} onChange={(e) => setAddBlank(e.currentTarget.checked)} />
          also try a blank password
        </label>
        <Show when={!scanner().userless}>
          <label class="checkline">
            <input type="checkbox" checked={addUserAsPass()}
              onChange={(e) => setAddUserAsPass(e.currentTarget.checked)} />
            also try each username as its own password
          </label>
        </Show>
      </div>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <Show when={plan().capped > 0 && !busy()}>
        <p class="mwarn" style="margin-top:12px">
          {plan().capped} combination{plan().capped === 1 ? "" : "s"} beyond the {BRUTE_CAP} run cap will not be tried.
        </p>
      </Show>
      <div class="mbtns">
        <button class="abtn" disabled={!valid() || busy()} onClick={() => void start()}>
          {busy() ? "Starting…" : `Launch ${plan().attempts.length} attempt${plan().attempts.length === 1 ? "" : "s"}`}
        </button>
      </div>
    </Modal>
  );
}
