import { Show, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";

export function UpgradeDialog(props: { sid: string; onClose: () => void }) {
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const valid = () => lhost().trim() !== "" && Number(lport()) > 0;

  async function upgrade() {
    setBusy(true);
    setError("");
    try {
      await ws.command("session.upgrade", {
        sid: props.sid, lhost: lhost().trim(), lport: Number(lport()),
      });
      props.onClose();
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Upgrade session ${props.sid} to meterpreter`} onClose={props.onClose} width="440px">
      <p class="mdesc">
        The framework spawns a meterpreter handler on this callback address and moves the shell over.
        A new session appears when it checks in.
      </p>
      <div class="frow" style="margin-top:14px">
        <label class="field"><b>LHOST</b>
          <input value={lhost()} placeholder="10.0.0.99"
            onInput={(e) => setLhost(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
        </label>
        <label class="field"><b>LPORT</b>
          <input value={lport()} inputmode="numeric"
            onInput={(e) => setLport(e.currentTarget.value)} autocomplete="off" />
        </label>
      </div>
      <Show when={error()}><p class="merror">{error()}</p></Show>
      <div class="mbtns">
        <button class="abtn" disabled={busy() || !valid()}
          onClick={() => void upgrade()}>
          {busy() ? "Upgrading…" : "Upgrade"}
        </button>
      </div>
    </Modal>
  );
}
