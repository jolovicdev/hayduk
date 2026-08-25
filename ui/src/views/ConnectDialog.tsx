import { createSignal, Show } from "solid-js";
import type { ConnectionState } from "../protocol/types";
import { HaydukMark } from "../components/mark";
import { events } from "../stores/events";
import { CONNECT_DEFAULTS, loadConnectDefaults, parsePort } from "./connectForm";

export default function ConnectDialog(props: {
  conn: ConnectionState;
  onConnect: (p: { host: string; port: number; ssl: boolean; username: string; password: string }) => Promise<unknown>;
}) {
  const [d, setD] = createSignal(loadConnectDefaults());
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const connecting = () => props.conn.status === "connecting";
  const portValid = () => parsePort(String(d().port)) !== undefined;
  const valid = () => d().host.trim() !== "" && portValid() && password() !== "";
  const progress = () => {
    if (!connecting()) return "";
    const last = events().at(-1);
    return last?.text ?? "reaching msfrpcd";
  };

  async function connect(e: Event) {
    e.preventDefault();
    if (connecting() || busy() || !valid()) return;
    setBusy(true);
    setError("");
    const p = { ...d(), host: d().host.trim(), password: password() };
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
      // settled is settled: the timer must not outlive a successful connect
      if (timer !== undefined) window.clearTimeout(timer);
      setBusy(false);
    }
  }

  return (
    <div class="modalback show">
      <form class="modal" style="width:380px" onSubmit={connect}>
        <div class="mhead">
          <HaydukMark size={30} />
          <div>
            <div class="mtitle">Connect to msfrpcd</div>
            <div class="mver">Metasploit operations, mapped.</div>
          </div>
        </div>

        <Show when={connecting()} fallback={
          <>
            <div style="margin-top:16px; display:grid; gap:10px">
              <label class="kv"><b>Host</b>
                <input value={d().host}
                  onInput={(e) => setD({ ...d(), host: e.currentTarget.value })} />
              </label>
              <div style="display:grid; grid-template-columns:1fr 1fr; gap:10px">
                <label class="kv"><b>Port</b>
                  <input type="number" value={d().port}
                    classList={{ invalid: !portValid() }}
                    onInput={(e) => setD({ ...d(), port: Number(e.currentTarget.value) })} />
                </label>
                <label class="kv"><b>User</b>
                  <input value={d().username}
                    onInput={(e) => setD({ ...d(), username: e.currentTarget.value })} />
                </label>
              </div>
              <label class="kv"><b>Password</b>
                <input type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} />
              </label>
              <label style="display:flex; gap:8px; align-items:center; font:400 12.5px var(--sans); color:var(--tx1)">
                <input type="checkbox" checked={d().ssl} onChange={(e) => setD({ ...d(), ssl: e.currentTarget.checked })} />
                use SSL (msfrpcd without -S)
              </label>
            </div>

            <Show when={props.conn.error || error()}>
              <p style="color:var(--red-br); margin-top:12px">{props.conn.error || error()}</p>
            </Show>

            <div class="mbtns">
              <button class="abtn" type="submit" disabled={busy() || !valid()} style="flex:none; padding:0 20px">
                {busy() ? "Connecting…" : "Connect"}
              </button>
            </div>
          </>
        }>
          <div style="margin-top:16px; display:grid; gap:8px; text-align:center">
            <p style="margin:0; color:var(--tx0)">Connecting to <b>{props.conn.host}:{props.conn.port}</b>…</p>
            <p style="margin:0; font:400 11.5px var(--mono); color:var(--tx2)">{progress()}</p>
            <p style="margin:6px 0 0; font:400 11px var(--sans); color:var(--tx2)">
              a cold msfrpcd can take half a minute to answer; the link state is always in the status bar
            </p>
          </div>
        </Show>
      </form>
    </div>
  );
}
