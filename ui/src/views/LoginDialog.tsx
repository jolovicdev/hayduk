import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { campaignState, credsByHostMemo } from "../stores/store";
import type { ServiceState } from "../protocol/types";
import { loginOptions, rankCreds } from "./login";

export function LoginDialog(props: { host: string; onClose: () => void }) {
  const services = createMemo(() =>
    campaignState().services.filter((s): s is ServiceState => !!s && s.host === props.host));
  const modules = createMemo(() => loginOptions(services()));
  const creds = createMemo(() => rankCreds(credsByHostMemo().get(props.host) ?? []));

  const [mod, setMod] = createSignal("");
  const chosenMod = () => modules().find(m => m.module === mod()) ?? modules()[0];
  // the fields are prefilled from the top-ranked cred, so the selector must
  // start on that entry too - not on "manual entry"
  const [credIdx, setCredIdx] = createSignal(creds().length > 0 ? 0 : -1);
  const [user, setUser] = createSignal(creds()[0]?.user ?? "");
  const [pass, setPass] = createSignal(creds()[0]?.pass ?? "");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  function pickCred(i: number) {
    setCredIdx(i);
    const c = creds()[i];
    setUser(c?.user ?? "");
    setPass(c?.pass ?? "");
  }

  async function launch() {
    const m = chosenMod();
    if (!m) return;
    setBusy(true);
    setError("");
    const opts: Record<string, unknown> = { RHOSTS: props.host };
    // attack the port the service was actually found on - even 139, which
    // smb_login does not default to
    if (m.port !== undefined) opts.RPORT = m.port;
    if (user().trim()) opts[m.userKey] = user().trim();
    if (pass()) opts[m.passKey] = pass();
    try {
      await ws.command("module.execute", { type: "auxiliary", name: m.module, options: opts });
      props.onClose();
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Login as ${props.host}`} onClose={props.onClose} width="480px">
      <Show when={modules().length > 0} fallback={
        <p class="merror">
          No SMB or SSH service known on this host. Scan it first, or pick a login module from the tree.
        </p>
      }>
        <label class="field" style="margin-top:4px">
          <b>Login module</b>
          <select value={chosenMod()?.module ?? ""} onChange={(e) => setMod(e.currentTarget.value)}>
            <For each={modules()}>{(m) => <option value={m.module}>{m.module}</option>}</For>
          </select>
        </label>

        <Show when={creds().length > 0}>
          <label class="field" style="margin-top:10px">
            <b>Recovered credentials <span class="fdesc">(from the creds table)</span></b>
            <select value={String(credIdx())} onChange={(e) => pickCred(Number(e.currentTarget.value))}>
              <option value="-1">Manual entry</option>
              <For each={creds()}>{(c, i) => (
                <option value={i()}>{`${c.user || "?"} / ${c.pass ? "•".repeat(Math.min(c.pass.length, 12)) : "no password"}`}</option>
              )}</For>
            </select>
          </label>
        </Show>

        <div class="frow even" style="margin-top:10px">
          <label class="field"><b>User</b>
            <input value={user()} onInput={(e) => setUser(e.currentTarget.value)}
              autocomplete="off" spellcheck={false} />
          </label>
          <label class="field"><b>Password</b>
            <input type="password" value={pass()} onInput={(e) => setPass(e.currentTarget.value)}
              autocomplete="off" />
          </label>
        </div>

        <Show when={error()}><p class="merror">{error()}</p></Show>

        <div class="mbtns">
          <button class="abtn" disabled={busy()} onClick={() => void launch()}>
            {busy() ? "Launching…" : "Launch login attack"}
          </button>
        </div>
      </Show>
    </Modal>
  );
}
