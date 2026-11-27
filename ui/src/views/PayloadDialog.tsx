import { Show, createEffect, createMemo, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { FilterSelect } from "../components/FilterSelect";
import { copyWithFeedback } from "../clipboard";
import { base64ToBytes, downloadBytes } from "../bytes";
import { errorMessage } from "../errors";
import type { PayloadGeneratePayload } from "../protocol/types";

const FORMATS = ["exe", "elf", "macho", "msi", "apk", "war", "jar", "raw",
  "base64", "hex", "c", "csharp", "java", "python", "powershell", "ps1", "ruby", "sh"];

export function PayloadDialog(props: { onClose: () => void; onWebDelivery?: () => void }) {
  const modules = createMemo(() => campaignState().modules);
  const payloads = createMemo(() => modules()?.payloads ?? []);
  const [payload, setPayload] = createSignal("");
  createEffect(() => {
    if (!payload() && payloads().length > 0) {
      setPayload(payloads().find(p => p.includes("linux/x64/meterpreter/reverse_tcp")) ?? payloads()[0]!);
    }
  });
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [format, setFormat] = createSignal("elf");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [result, setResult] = createSignal<PayloadGeneratePayload | null>(null);

  const valid = () => payload() !== "" && lhost().trim() !== "" && Number(lport()) > 0;

  async function generate() {
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const res = await ws.command<PayloadGeneratePayload>(
        "payload.generate",
        {
          payload: payload(), format: format(),
          options: { LHOST: lhost().trim(), LPORT: Number(lport()) },
        },
      );
      setResult(res);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Generate payload" onClose={props.onClose} width="520px">
      <p class="mdesc">
        Generates with the framework payload engine and returns the artifact straight
        to your browser, even when msfrpcd runs on another machine. Start a matching
        listener before it runs.
      </p>
      <div class="mform">
        <div class="field">
          <b>Payload</b>
          <FilterSelect label="payloads" options={payloads().map(p => ({ value: p }))} value={payload()} onChange={setPayload} />
        </div>
        <div class="frow">
          <label class="field"><b>LHOST</b>
            <input value={lhost()} onInput={(e) => setLhost(e.currentTarget.value)}
              placeholder="192.168.1.10" autocomplete="off" spellcheck={false} />
          </label>
          <label class="field"><b>LPORT</b>
            <input value={lport()} inputmode="numeric" onInput={(e) => setLport(e.currentTarget.value)} />
          </label>
        </div>
        <label class="field"><b>Format</b>
          <select value={format()} onChange={(e) => setFormat(e.currentTarget.value)}>
            {FORMATS.map(f => <option value={f}>{f}</option>)}
          </select>
        </label>
      </div>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <Show when={result()}>
        {(r) => (
          <div class="msec">
            <Show when={r().text}>
              <pre class="toolout" style="max-height:160px">{r().text}</pre>
            </Show>
            <div class="mbtns" style="margin-top:8px">
              <Show when={r().dataBase64}>
                <button class="abtn primary"
                  onClick={() => downloadBytes(r().filename, base64ToBytes(r().dataBase64!))}>
                  <i aria-hidden="true" class="ph ph-download-simple"></i>
                  Download {r().filename} ({r().size} bytes)
                </button>
              </Show>
              <Show when={r().text}>
                <button class="abtn" onClick={() => copyWithFeedback(r().text!)}>
                  <i aria-hidden="true" class="ph ph-copy"></i>Copy text
                </button>
              </Show>
            </div>
          </div>
        )}
      </Show>
      <div class="mbtns">
        <Show when={props.onWebDelivery}>
          <button class="abtn" onClick={props.onWebDelivery}>
            <i aria-hidden="true" class="ph ph-globe"></i>Web delivery…
          </button>
        </Show>
        <button class="abtn" disabled={busy() || !valid()}
          onClick={() => void generate()}>
          {busy() ? "Generating…" : "Generate"}
        </button>
      </div>
    </Modal>
  );
}
