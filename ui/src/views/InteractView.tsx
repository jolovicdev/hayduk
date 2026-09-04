import { Show } from "solid-js";
import { ConsoleView } from "../components/ConsoleView";
import { detach, interactOutput, interactSID, write } from "../stores/interact";
import { campaignState } from "../stores/store";
import { flash } from "../statusflash";
import { interactPrompt } from "./format";

export function InteractView() {
  const session = () => campaignState().sessions[interactSID()];

  return (
    <Show when={interactSID()} fallback={
      <div class="console">
        <div class="cl dim">No session attached. Right-click a session or host and choose Interact.</div>
      </div>
    }>
      <div style="padding:8px 12px 0; display:flex; gap:8px; align-items:center">
        <i class="ph ph-broadcast" style="color:var(--grn-tx)"></i>
        <span style="font:600 12.5px var(--sans)">
          session {interactSID()} · {session()?.type ?? ""} · {session()?.username || session()?.targetHost || ""}
        </span>
        <span style="flex:1"></span>
        <button class="abtn" style="flex:none; padding:0 12px"
          onClick={() => void detach().catch((e: any) => flash(e?.message ?? "detach failed"))}>
          <i class="ph ph-x"></i>Detach
        </button>
      </div>
      <ConsoleView output={interactOutput}
        prompt={interactPrompt(session()?.type, interactSID())} busy={false}
        write={(cmd) => void write(cmd).catch((e: any) => flash(e?.message ?? "session write failed"))}
        tabComplete={async () => []} />
    </Show>
  );
}
