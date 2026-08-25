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
    <Modal title={`Login as… ${props.host}`} onClose={props.onClose} width="480px">
      <Show when={modules().length > 0} fallback={
        <p style="color:var(--red-br); margin-top:4px">
          No SMB or SSH service known on this host. Scan it first, or pick a login module from the tree.
        </p>
      }>
        <label style="margin-top:4px; display:grid; gap:4px">
          <span style="font:500 11px var(--sans); color:var(--tx1)">Login module</span>
          <select value={chosenMod()?.module ?? ""} onChange={(e) => setMod(e.currentTarget.value)}>
            <For each={modules()}>{(m) => <option value={m.module}>{m.module}</option>}</For>
          </select>
        </label>

        <Show when={creds().length > 0}>
          <label style="margin-top:10px; display:grid; gap:4px">
            <span style="font:500 11px var(--sans); color:var(--tx1)">
              Recovered credentials <span style="color:var(--tx2)">(from the creds table)</span>
            </span>
            <select value={String(credIdx())} onChange={(e) => pickCred(Number(e.currentTarget.value))}>
              <option value="-1">Manual entry</option>
              <For each={creds()}>{(c, i) => (
                <option value={i()}>{`${c.user || "?"} / ${c.pass ? "•".repeat(Math.min(c.pass.length, 12)) : "no password"}`}</option>
              )}</For>
            </select>
          </label>
        </Show>

        <div style="margin-top:10px; display:grid; grid-template-columns:1fr 1fr; gap:10px">
          <label style="display:grid; gap:4px">
            <span style="font:500 11px var(--sans); color:var(--tx1)">User</span>
            <input value={user()} onInput={(e) => setUser(e.currentTarget.value)}
              autocomplete="off" spellcheck={false} />
          </label>
          <label style="display:grid; gap:4px">
            <span style="font:500 11px var(--sans); color:var(--tx1)">Password</span>
            <input type="password" value={pass()} onInput={(e) => setPass(e.currentTarget.value)}
              autocomplete="off" />
          </label>
        </div>

        <Show when={error()}><p style="color:var(--red-br); margin-top:12px">{error()}</p></Show>

        <div class="mbtns">
          <button class="abtn" style="flex:none; padding:0 20px" disabled={busy()} onClick={() => void launch()}>
            {busy() ? "Launching…" : "Launch login attack"}
          </button>
        </div>
      </Show>
    </Modal>
  );
}
