import { For, Show, createResource, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { FilterSelect } from "../components/FilterSelect";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { campaignState } from "../stores/store";
import type { AttacksPayload } from "../protocol/types";

export function FindAttacksDialog(props: {
  host?: string;
  onLaunch: (path: string, host: string, port: number) => void;
  onClose: () => void;
}) {
  const hosts = () => campaignState().hosts.filter(h => !!h).map(h => h!.address);
  const initial = props.host && hosts().includes(props.host) ? props.host : hosts()[0];
  const [host, setHost] = createSignal(initial);

  const [result] = createResource(host, async (h) => {
    if (!h) return null;
    return ws.command<AttacksPayload>("attacks.find", { host: h });
  });

  const errorText = () => {
    if (hosts().length === 0) return "";
    if (result.loading) return "";
    const e = result.error;
    if (!e) return "";
    return e instanceof CommandError ? `${e.code}: ${e.message}` : String(e);
  };

  return (
    <Modal title="Find attacks" onClose={props.onClose} width="600px">
      <p style="margin-top:4px; font:400 12px/1.55 var(--sans); color:var(--tx2)">
        Matches use service names in module paths and the host's OS family.
        Versions and patch levels are not checked. Verify compatibility before launching.
      </p>
      <Show when={hosts().length > 0} fallback={
        <p style="color:var(--red-br); margin-top:14px">
          No hosts in the workspace yet; discover hosts first.
        </p>
      }>
        <div style="margin-top:14px; display:grid; gap:4px">
          <span style="font:500 11px var(--sans); color:var(--tx1)">Host</span>
          <FilterSelect options={hosts().map(h => ({ value: h }))}
            value={host() ?? ""} label="hosts" onChange={setHost} />
        </div>

        <Show when={result.loading}>
          <div class="opt-skeleton" aria-hidden="true">
            <div class="skel"></div>
            <div class="skel"></div>
            <div class="skel short"></div>
          </div>
        </Show>
        <Show when={!result.loading && result.latest}>
          {(r) => (
            <div style="margin-top:14px; display:grid; gap:6px; max-height:320px; overflow:auto; padding-right:6px">
              <For each={r().matches} fallback={
                <p style="color:var(--tx2); text-align:center; padding:12px 0">No exploits matched this host's services.</p>
              }>
                {(m) => (
                  <div class="fmatch">
                    <div class="fminfo">
                      <span class="fmname">{m.name}</span>
                      <span class="fmreason">{m.reason}</span>
                    </div>
                    <button class="abtn" style="flex:none"
                      onClick={() => props.onLaunch(m.name, r().host, m.port)}>
                      <i class="ph ph-rocket-launch"></i>Launch
                    </button>
                  </div>
                )}
              </For>
            </div>
          )}
        </Show>
        <Show when={errorText()}><p style="color:var(--red-br); margin-top:10px">{errorText()}</p></Show>
      </Show>
    </Modal>
  );
}
