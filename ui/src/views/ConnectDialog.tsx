import { createSignal, Show } from "solid-js";
import type { ConnectionState } from "../protocol/types";
import { HaydukMark } from "../components/mark";
import { events } from "../stores/events";
import { CONNECT_DEFAULTS, friendlyConnectError, loadConnectDefaults, parsePort } from "./connectForm";

export default function ConnectDialog(props: {
  conn: ConnectionState;
  onConnect: (p: { host: string; port: number; ssl: boolean; username: string; password: string }) => Promise<unknown>;
}) {
  // port stays a string: per-keystroke Number() renders "NaN" into the box
  const loaded = loadConnectDefaults();
  const [d, setD] = createSignal({ ...loaded, port: String(loaded.port) });
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const connecting = () => props.conn.status === "connecting";
  const portValid = () => parsePort(d().port) !== undefined;
  const valid = () => d().host.trim() !== "" && portValid() && password() !== "";
  const progress = () => {
    if (!connecting()) return "";
    const last = events().at(-1);
    return last?.text ?? "reaching msfrpcd";
  };

  async function connect(e: Event) {
    e.preventDefault();
    const port = parsePort(d().port);
    if (connecting() || busy() || !valid() || port === undefined) return;
    setBusy(true);
    setError("");
    const p = { ...d(), host: d().host.trim(), port, password: password() };
    for (const k of Object.keys(CONNECT_DEFAULTS) as (keyof typeof CONNECT_DEFAULTS)[]) {
      localStorage.setItem("hayduk." + k, String((p as any)[k]));
    }
    // the engine keeps trying regardless; the race only stops this promise
    // from waiting forever if something wedges between us and the server
    let timer: number | undefined;
    try {
      await Promise.race([
        props.onConnect(p),
        new Promise((_, reject) => {
          timer = window.setTimeout(() => reject(new Error("timeout")), 120_000);
        }),
      ]);
    } catch (err: any) {
      setError(err.message ?? String(err));
    } finally {
      if (timer !== undefined) window.clearTimeout(timer);
      setBusy(false);
    }
  }

  return (
    <div class="modalback show">
      <form class="modal connect-modal" aria-labelledby="connect-title" onSubmit={connect}>
        <div class="mhead">
          <HaydukMark size={30} />
          <div>
            <div class="eyebrow">Metasploit connection</div>
            <div class="mtitle" id="connect-title">Connect to msfrpcd</div>
          </div>
        </div>

        <p>Enter the host, port, and credentials for your Metasploit RPC server.</p>
        <Show when={connecting()} fallback={
          <>
            <div class="mform">
              <label class="field"><b>Host</b>
                <input value={d().host}
                  onInput={(e) => setD({ ...d(), host: e.currentTarget.value })} />
              </label>
              <div class="frow even">
                <label class="field"><b>Port</b>
                  <input inputmode="numeric" value={d().port}
                    classList={{ invalid: !portValid() }}
                    onInput={(e) => setD({ ...d(), port: e.currentTarget.value })} />
                </label>
                <label class="field"><b>User</b>
                  <input value={d().username}
                    onInput={(e) => setD({ ...d(), username: e.currentTarget.value })} />
                </label>
              </div>
              <label class="field"><b>Password</b>
                <input type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} />
              </label>
              <label class="checkline">
                <input type="checkbox" checked={d().ssl} onChange={(e) => setD({ ...d(), ssl: e.currentTarget.checked })} />
                use SSL (msfrpcd without -S)
              </label>
            </div>

            <Show when={props.conn.error || error()}>
              {(_) => {
                const mapped = friendlyConnectError(props.conn.error || error());
                return <>
                  <p role="alert" class="merror">{mapped.primary}</p>
                  <Show when={mapped.detail}>
                    <p class="mnote" style="margin-top:2px">
                      {mapped.detail}
                    </p>
                  </Show>
                </>;
              }}
            </Show>

            <div class="mbtns">
              <button class="abtn" type="submit" disabled={busy() || !valid()}>
                {busy() ? "Connecting…" : "Connect"}
              </button>
            </div>
          </>
        }>
          <div style="margin-top:16px; display:grid; gap:8px; text-align:center">
            <p style="margin:0; color:var(--tx0)">Connecting to <b>{props.conn.host}:{props.conn.port}</b>…</p>
            <p class="mnote" style="margin:0">{progress()}</p>
            <p class="mdesc" style="margin:6px 0 0">
              a cold msfrpcd can take half a minute to answer; the link state is always in the status bar
            </p>
          </div>
        </Show>
      </form>
    </div>
  );
}
