import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { FilterSelect } from "../components/FilterSelect";
import { ws } from "../ws/singleton";
import { campaignState, credsByHostMemo } from "../stores/store";
import type { ServiceState } from "../protocol/types";
import { loginOptions, looksLikeNtlmHash, rankCreds } from "./login";
import { errorMessage } from "../errors";

export function LoginDialog(props: {
  host: string;
  // initialCred preselects one recovered credential (CredsView's Pass the
  // Hash action) instead of the host's top-ranked one
  initialCred?: { user?: string; pass?: string };
  onClose: () => void;
}) {
  const services = createMemo(() =>
    campaignState().services.filter((s): s is ServiceState => !!s && s.host === props.host));
  const modules = createMemo(() => loginOptions(services()));
  const creds = createMemo(() => rankCreds(credsByHostMemo().get(props.host) ?? []));

  const [mod, setMod] = createSignal("");
  const chosenMod = () => modules().find(m => m.module === mod()) ?? modules()[0];
  const isSession = () => chosenMod()?.kind === "session";
  // the fields are prefilled from the top-ranked cred, so the selector must
  // start on that entry too - not on "manual entry"
  const seed = props.initialCred ?? creds()[0];
  const [credIdx, setCredIdx] = createSignal(props.initialCred ? -1 : (creds().length > 0 ? 0 : -1));
  const [user, setUser] = createSignal(seed?.user ?? "");
  const [pass, setPass] = createSignal(seed?.pass ?? "");
  const [domain, setDomain] = createSignal("");
  const [payload, setPayload] = createSignal("");
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const chosenPayload = () => payload() || chosenMod()?.payloads?.[0] || "";
  const pth = () => looksLikeNtlmHash(pass()) && !!chosenMod()?.domainKey;
  const sessionReady = () => !isSession() || (lhost().trim() !== "" && Number(lport()) > 0 && !!chosenPayload());

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
    if (m.domainKey && domain().trim()) opts[m.domainKey] = domain().trim();
    try {
      await ws.command("module.execute", {
        type: m.moduleType, name: m.module, options: opts,
        ...(m.kind === "session" ? {
          payload: chosenPayload(),
          payloadOptions: { LHOST: lhost().trim(), LPORT: Number(lport()) },
        } : {}),
      });
      props.onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Login / Lateral · ${props.host}`}
      onClose={props.onClose} width="500px">
      <Show when={modules().length > 0} fallback={
        <p class="merror">
          No SMB, SSH, or WinRM service known on this host. Scan it first, or pick a login module from the tree.
        </p>
      }>
        <label class="field" style="margin-top:4px">
          <b>Module</b>
          <select value={chosenMod()?.module ?? ""} onChange={(e) => setMod(e.currentTarget.value)}>
            <For each={modules()}>{(m) => (
              <option value={m.module}>{m.label}{m.port ? ` · port ${m.port}` : ""}</option>
            )}</For>
          </select>
          <Show when={chosenMod()?.port}>
            <span class="fdesc">targets the port this host's scan found</span>
          </Show>
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
          <label class="field"><b>Password or NTLM hash</b>
            <input type="password" value={pass()} onInput={(e) => setPass(e.currentTarget.value)}
              autocomplete="off" />
          </label>
        </div>
        <Show when={chosenMod()?.domainKey}>
          <label class="field" style="margin-top:10px"><b>Domain <span class="fdesc">(optional, e.g. CORP)</span></b>
            <input value={domain()} onInput={(e) => setDomain(e.currentTarget.value)}
              autocomplete="off" spellcheck={false} />
          </label>
        </Show>
        <Show when={pth()}>
          <p class="mnote">NTLM hash detected: this launches as Pass the Hash.</p>
        </Show>

        <Show when={isSession()}>
          <div class="field" style="margin-top:10px">
            <b>Payload</b>
            <FilterSelect label="payloads" options={(chosenMod()?.payloads ?? []).map(p => ({ value: p }))}
              value={chosenPayload()} onChange={setPayload} />
          </div>
          <div class="frow even" style="margin-top:10px">
            <label class="field"><b>LHOST <span class="fdesc">(callback to you)</span></b>
              <input value={lhost()} onInput={(e) => setLhost(e.currentTarget.value)}
                placeholder="192.168.1.10" autocomplete="off" spellcheck={false} />
            </label>
            <label class="field"><b>LPORT</b>
              <input value={lport()} inputmode="numeric" onInput={(e) => setLport(e.currentTarget.value)} />
            </label>
          </div>
          <p class="mnote" style="margin-top:8px">Start a matching listener first; the session lands in Sessions when it calls back.</p>
        </Show>

        <Show when={error()}><p class="merror">{error()}</p></Show>

        <div class="mbtns">
          <button class="abtn primary" disabled={busy() || !sessionReady()} onClick={() => void launch()}>
            {busy() ? "Launching…" : isSession() ? "Launch session attack" : "Launch login attack"}
          </button>
        </div>
      </Show>
    </Modal>
  );
}
