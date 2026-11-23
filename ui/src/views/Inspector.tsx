import { For, Show, createMemo } from "solid-js";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { serviceOpen } from "./serviceState";

export function Inspector(props: {
  addr: () => string | undefined;
  onInteract: (sid: string) => void;
  onLogin: (host: string) => void;
  onPivot: (sid: string) => void;
}) {
  const host = createMemo(() => campaignState().hosts.find(h => h?.address === props.addr()));
  const sessions = createMemo(() => {
    const h = host();
    return h ? (sessionsByHostMemo().get(h.address) ?? []) : [];
  });
  const creds = createMemo(() => {
    const h = host();
    return h ? (credsByHostMemo().get(h.address) ?? []) : [];
  });
  // only reachable rows: the count is labelled "Open services" and closed
  // ports are noise in an attack summary
  const services = createMemo(() => {
    const h = host();
    return h ? campaignState().services.filter(s => s?.host === h.address && serviceOpen(s)) : [];
  });

  return (
    <div class="ins">
      <Show when={host()} fallback={
        <div class="ins-empty">
          <div class="empty-symbol"><i aria-hidden="true" class="ph ph-target"></i></div>
          <h2>No host selected</h2>
          <p>Select a host on the topology map or in Services to inspect ports, sessions, and actions.</p>
        </div>
      }>
        {(h) => <>
          <div class="ihost">
            <div>
              <div class="iname">{h().name || h().address}</div>
              <div class="ios">
                {[h().osName, h().osFlavor, h().osVersion].filter(Boolean).join(" ") || "unknown os"}
              </div>
            </div>
          </div>

          <div class="badges">
            <Show when={sessions().length > 0} fallback={
              creds().length > 0
                ? <span class="bdg amb"><i aria-hidden="true" class="ph ph-key"></i>login available</span>
                : <span class="bdg gry"><i aria-hidden="true" class="ph ph-x"></i>no access</span>
            }>
              <span class="bdg grn"><i aria-hidden="true" class="ph-fill ph-lightning"></i>access obtained</span>
              <span class="bdg grn"><i aria-hidden="true" class="ph ph-pulse"></i>{sessions().length} live</span>
            </Show>
          </div>

          <Show when={h().comments}>
            <div class="inote">{h().comments}</div>
          </Show>

          <div class="isec">
            <div class="ilabel">Host</div>
            <div class="kv"><b>Address</b><span>{h().address}</span></div>
            <Show when={h().mac}><div class="kv"><b>MAC</b><span>{h().mac}</span></div></Show>
            <div class="kv"><b>Open services</b><span>{services().length}</span></div>
          </div>

          <div class="isec">
            <div class="ilabel">Open services</div>
            <Show when={services().length > 0} fallback={
              <div class="ios" style="margin-top:0">No open ports recorded</div>
            }>
              <For each={services()}>{(s) => (
                <div class="portrow">
                  <span class="pchip">{s!.port}/{s!.proto}</span>
                  <span class="pn">{s!.name}</span>
                  <span class="pi" title={s!.info}>{s!.info}</span>
                </div>
              )}</For>
            </Show>
          </div>

          <div class="isec">
            <div class="ilabel">Access</div>
            <div class="acc-list">
              <Show when={sessions().length > 0} fallback={
                <div class="acc">
                  <div class="aline">
                    <b>{creds().length > 0 ? "Valid credentials found" : "No active session"}</b>
                  </div>
                  <div class="abtns">
                    <button class="abtn" onClick={() => props.onLogin(h().address)}>
                      <i aria-hidden="true" class="ph ph-key"></i>Login as…
                    </button>
                  </div>
                </div>
              }>
                <For each={sessions()}>{(s) => (
                  <div class="acc live-acc">
                    <div class="aline"><b>Session {s!.id}</b> · {s!.type}</div>
                    <div class="aline">{s!.username || "unknown user"}</div>
                    <Show when={s!.viaExploit}>
                      <div class="aline" style="color:var(--tx2)">via {s!.viaExploit}</div>
                    </Show>
                    <div class="abtns">
                      <button class="abtn primary" onClick={() => props.onInteract(s!.id)}>
                        <i aria-hidden="true" class="ph ph-terminal-window"></i>Interact
                      </button>
                      <button class="abtn" onClick={() => props.onPivot(s!.id)}>
                        <i aria-hidden="true" class="ph ph-signpost"></i>Pivot
                      </button>
                    </div>
                  </div>
                )}</For>
              </Show>
            </div>
          </div>
        </>}
      </Show>
    </div>
  );
}
