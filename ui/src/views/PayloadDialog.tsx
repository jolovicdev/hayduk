import { Show, createEffect, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { FilterSelect } from "../components/FilterSelect";

const FORMATS = ["exe", "elf", "macho", "msi", "apk", "war", "jar", "raw",
  "base64", "hex", "c", "csharp", "java", "python", "powershell", "ps1", "ruby", "sh"];

export function PayloadDialog(props: { onClose: () => void }) {
  const payloads = () => campaignState().modules?.payloads ?? [];
  createEffect(() => {
    if (!payload() && payloads().length > 0) {
      setPayload(payloads().find(p => p.includes("linux/x64/meterpreter/reverse_tcp")) ?? payloads()[0]!);
    }
  });
  const [payload, setPayload] = createSignal("");
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [format, setFormat] = createSignal("base64");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [result, setResult] = createSignal<{ filename: string; content?: string; savedPath?: string } | null>(null);

  const valid = () => payload() !== "" && lhost().trim() !== "" && Number(lport()) > 0;

  async function generate() {
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const res = await ws.command<{ filename: string; content?: string; savedPath?: string }>(
        "payload.generate",
        {
          payload: payload(), format: format(),
          options: { LHOST: lhost().trim(), LPORT: Number(lport()) },
        },
      );
      setResult(res);
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title="Generate payload" onClose={props.onClose} width="520px">
      <p class="mdesc">
        Generates with the framework payload engine and writes the result to a
        file on the msfrpcd host; copy it to the target from the reported path.
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
          <p class="mtext" style="margin-top:12px">
            Saved on the msfrpcd host at <b>{r().savedPath}</b>. Copy it to the target,
            and start a matching listener before it runs.
          </p>
        )}
      </Show>
      <div class="mbtns">
        <button class="abtn" disabled={busy() || !valid()}
          onClick={() => void generate()}>
          {busy() ? "Generating…" : "Generate"}
        </button>
      </div>
    </Modal>
  );
}
