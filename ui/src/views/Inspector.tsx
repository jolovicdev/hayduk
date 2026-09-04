import { For, Show, createMemo } from "solid-js";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { serviceOpen } from "./serviceState";

export function Inspector(props: {
  addr: () => string | undefined;
  onInteract: (sid: string) => void;
  onLogin: (host: string) => void;
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
      <Show when={host()} fallback={<div class="inote">Select a host to inspect it.</div>}>
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
                ? <span class="bdg amb"><i class="ph ph-key"></i>valid login found</span>
                : <span class="bdg gry"><i class="ph ph-x"></i>no access</span>
            }>
              <span class="bdg red"><i class="ph-fill ph-lightning"></i>access obtained</span>
              <span class="bdg grn"><i class="ph ph-pulse"></i>{sessions().length} session{sessions().length > 1 ? "s" : ""} live</span>
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
            <div class="ilabel">Services</div>
            <For each={services()}>{(s) => (
              <div class="portrow">
                <span class="pchip">{s!.port}/{s!.proto}</span>
                <span class="pn">{s!.name}</span>
                <span class="pi">{s!.info}</span>
              </div>
            )}</For>
          </div>

          <div class="isec">
            <div class="ilabel">Access</div>
            <div class="acc">
              <Show when={sessions().length > 0} fallback={
                <div class="aline">
                  <b>{creds().length > 0 ? "Valid credentials found" : "No access on this host"}</b>
                  <div class="abtns">
                    <button class="abtn" onClick={() => props.onLogin(h().address)}>
                      <i class="ph ph-key"></i>Login as…
                    </button>
                  </div>
                </div>
              }>
                <For each={sessions()}>{(s) => (
                  <>
                    <div class="aline"><b>Session {s!.id}</b> · {s!.type}</div>
                    <div class="aline">{s!.username || "unknown user"}</div>
                    <div class="aline">via {s!.viaExploit}</div>
                    <div class="abtns">
                      <button class="abtn" onClick={() => props.onInteract(s!.id)}>
                        <i class="ph ph-terminal-window"></i>Interact
                      </button>
                    </div>
                  </>
                )}</For>
              </Show>
            </div>
          </div>
        </>}
      </Show>
    </div>
  );
}
