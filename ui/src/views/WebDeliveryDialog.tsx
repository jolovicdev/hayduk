import { For, Show, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { FilterSelect } from "../components/FilterSelect";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { copyWithFeedback } from "../clipboard";
import { errorMessage } from "../errors";
import { WEB_DELIVERY_PLATFORMS, defaultPlatform, webDeliveryReady } from "./webDelivery";
import type { WebDeliveryPayload } from "../protocol/types";

export function WebDeliveryDialog(props: { onClose: () => void }) {
  const payloads = createMemo(() => campaignState().modules?.payloads ?? []);
  const [platform, setPlatform] = createSignal("python");
  const [payload, setPayload] = createSignal(defaultPlatform("python").payload);
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [srvPort, setSrvPort] = createSignal("8080");
  const [srvHost, setSrvHost] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [result, setResult] = createSignal<WebDeliveryPayload | null>(null);

  function pickPlatform(value: string) {
    setPlatform(value);
    setPayload(defaultPlatform(value).payload);
  }

  async function launch() {
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const res = await ws.command<WebDeliveryPayload>("web.delivery", {
        payload: payload(), target: platform(),
        lhost: lhost().trim(), lport: Number(lport()), srvPort: Number(srvPort()),
        ...(srvHost().trim() ? { srvHost: srvHost().trim() } : {}),
      });
      setResult(res);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Web delivery" onClose={props.onClose} width="560px">
      <p class="mdesc">
        Starts exploit/multi/script/web_delivery: a web server that hands the payload
        to a one-liner you run on the target. Nothing is written to the target's disk
        by the launcher itself.
      </p>
      <div class="mform">
        <label class="field">
          <b>Target platform</b>
          <select value={platform()} onChange={(e) => pickPlatform(e.currentTarget.value)}>
            <For each={WEB_DELIVERY_PLATFORMS}>{(p) => <option value={p.value}>{p.label}</option>}</For>
          </select>
        </label>
        <div class="field">
          <b>Payload</b>
          <FilterSelect label="payloads" options={payloads().map(p => ({ value: p }))}
            value={payload()} onChange={setPayload} />
        </div>
        <div class="frow">
          <label class="field"><b>LHOST <span class="fdesc">(callback address)</span></b>
            <input value={lhost()} onInput={(e) => setLhost(e.currentTarget.value)}
              placeholder="192.168.1.10" autocomplete="off" spellcheck={false} />
          </label>
          <label class="field"><b>LPORT</b>
            <input value={lport()} inputmode="numeric" onInput={(e) => setLport(e.currentTarget.value)} />
          </label>
        </div>
        <div class="frow">
          <label class="field"><b>Web host <span class="fdesc">(optional; default LHOST)</span></b>
            <input value={srvHost()} onInput={(e) => setSrvHost(e.currentTarget.value)}
              placeholder="same as LHOST" autocomplete="off" spellcheck={false} />
          </label>
          <label class="field"><b>Web port</b>
            <input value={srvPort()} inputmode="numeric" onInput={(e) => setSrvPort(e.currentTarget.value)} />
          </label>
        </div>
      </div>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <Show when={result()}>
        {(r) => (
          <div class="msec">
            <p class="mtext">
              Serving <b>{r().url}</b> as job {r().jobId}. Run this on the target:
            </p>
            <pre class="toolout" style="max-height:160px">{r().command}</pre>
            <div class="mbtns" style="margin-top:8px">
              <button class="abtn" onClick={() => copyWithFeedback(r().command)}>
                <i aria-hidden="true" class="ph ph-copy"></i>Copy one-liner
              </button>
              <button class="abtn" onClick={() => copyWithFeedback(r().url)}>Copy URL</button>
            </div>
          </div>
        )}
      </Show>
      <div class="mbtns">
        <button class="abtn primary"
          disabled={busy() || !webDeliveryReady({ payload: payload(), lhost: lhost(), lport: lport(), srvPort: srvPort() })}
          onClick={() => void launch()}>
          {busy() ? "Starting…" : result() ? "Start another" : "Start web delivery"}
        </button>
      </div>
    </Modal>
  );
}
