import { For, Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { flash } from "../statusflash";
import {
  BRUTE_CAP, LOGIN_SCANNERS, attemptOptions, buildBrutePlan, parseLines,
} from "./brute";

const DEFAULT_USERS = "Administrator\nadmin\nuser";
const DEFAULT_PASSES = "password\n123456\nadmin\nuser\ntest";

export function BruteDialog(props: { host?: string; onClose: () => void }) {
  const [hosts, setHosts] = createSignal(props.host ?? "");
  const [service, setService] = createSignal(LOGIN_SCANNERS[0]!.service);
  const [users, setUsers] = createSignal(DEFAULT_USERS);
  const [passes, setPasses] = createSignal(DEFAULT_PASSES);
  const [addBlank, setAddBlank] = createSignal(true);
  const [addUserAsPass, setAddUserAsPass] = createSignal(false);
  const [error, setError] = createSignal("");
  const [done, setDone] = createSignal(0);
  const [current, setCurrent] = createSignal("");
  const [running, setRunning] = createSignal(false);
  const [stopped, setStopped] = createSignal(false);

  const scanner = () => LOGIN_SCANNERS.find(s => s.service === service()) ?? LOGIN_SCANNERS[0]!;
  const hostList = () => parseLines(hosts());
  const userList = () => (scanner().userless ? [] : parseLines(users()));
  const passList = () => parseLines(passes());

  const plan = () => buildBrutePlan({
    hosts: hostList(), users: userList(), passes: passList(),
    module: scanner().module, userless: scanner().userless,
    addBlank: addBlank(), addUserAsPass: addUserAsPass(),
    cap: BRUTE_CAP,
  });
  const valid = () => hostList().length > 0 && passList().length + (addBlank() ? 1 : 0) > 0
    && (scanner().userless || userList().length > 0);

  async function run() {
    const p = plan();
    if (p.attempts.length === 0) return;
    setRunning(true);
    setStopped(false);
    setDone(0);
    setError("");
    const credsBefore = campaignState().creds.length;
    let next = 0;
    let failure = "";
    // the scanners answer one pair at a time; a few workers keep the spray
    // moving without flooding the daemon
    const workers = Array.from({ length: Math.min(3, p.attempts.length) }, async () => {
      while (!stopped()) {
        const i = next++;
        const a = p.attempts[i];
        if (!a) break;
        setCurrent(`${a.user || "(blank)"} @ ${a.host}`);
        try {
          await ws.command("module.execute", {
            type: "auxiliary", name: p.module, options: attemptOptions(a, scanner().userless),
          });
        } catch (e) {
          failure = e instanceof CommandError ? `${e.code}: ${e.message}` : String(e);
        }
        setDone(d => d + 1);
      }
    });
    await Promise.all(workers);
    setRunning(false);
    setCurrent("");
    const found = campaignState().creds.length - credsBefore;
    if (found > 0) {
      flash(`${found} credential${found === 1 ? "" : "s"} found - see the Credentials tab`);
    } else if (failure) {
      setError(failure);
    } else {
      flash(`brute force finished: ${p.attempts.length} attempt${p.attempts.length === 1 ? "" : "s"}, no credentials`);
    }
    props.onClose();
  }

  return (
    <Modal title="Brute force logins" onClose={props.onClose} width="560px">
      <p class="mdesc">
        Sprays credential pairs at a login service and stores anything that works in the
        workspace. Verify authorization before pointing this at a host.
      </p>
      <div class="mform">
        <label class="field">
          <b>Hosts <span class="fdesc">(one per line)</span></b>
          <textarea rows={3} value={hosts()} spellcheck={false}
            placeholder="10.0.0.5" onInput={(e) => setHosts(e.currentTarget.value)} />
        </label>
        <label class="field">
          <b>Service</b>
          <select value={service()} onChange={(e) => setService(e.currentTarget.value)}>
            <For each={LOGIN_SCANNERS}>{(s) => <option value={s.service}>{s.label}</option>}</For>
          </select>
        </label>
        <div class="frow even">
          <label class="field">
            <b>Users <span class="fdesc">(one per line)</span></b>
            <textarea rows={4} value={users()} spellcheck={false} disabled={scanner().userless}
              onInput={(e) => setUsers(e.currentTarget.value)} />
          </label>
          <label class="field">
            <b>Passwords <span class="fdesc">(one per line)</span></b>
            <textarea rows={4} value={passes()} spellcheck={false}
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
      <Show when={running()}>
        <p class="mnote" style="margin-top:12px">
          {done()}/{plan().attempts.length} attempts · {current()}
        </p>
      </Show>
      <Show when={plan().capped > 0 && !running()}>
        <p class="mwarn" style="margin-top:12px">
          {plan().capped} combination{plan().capped === 1 ? "" : "s"} beyond the {BRUTE_CAP} run cap will not be tried.
        </p>
      </Show>
      <div class="mbtns">
        <Show when={running()} fallback={
          <button class="abtn" disabled={!valid()} onClick={() => void run()}>
            Launch {plan().attempts.length} attempt{plan().attempts.length === 1 ? "" : "s"}
          </button>
        }>
          <button class="abtn" onClick={() => setStopped(true)}>Stop</button>
        </Show>
      </div>
    </Modal>
  );
}
