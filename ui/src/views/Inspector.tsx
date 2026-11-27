import { For, Show, createMemo } from "solid-js";
import { campaignState, credsByHostMemo, servicesByHostMemo, sessionsByHostMemo } from "../stores/store";
import { copyWithFeedback } from "../clipboard";
import { OsIconBadge, detectOsFamily } from "../components/OsIcon";
import { formatRhosts } from "../components/graph/selection";
import { serviceOpen } from "./serviceState";

export function Inspector(props: {
  selectedHosts: () => string[];
  onSelectOne: (addr: string) => void;
  onClearSelection: () => void;
  onInteract: (sid: string) => void;
  onLogin: (hosts: string[]) => void;
  onPivot: (sid: string) => void;
  onScan?: (target: string) => void;
  onFindAttacks?: (host: string) => void;
  onBrute?: (hosts: string[]) => void;
  onHailMary?: (hosts: string[]) => void;
}) {
  const multiHosts = createMemo(() => {
    const picked = new Set(props.selectedHosts());
    if (picked.size <= 1) return [];
    return campaignState().hosts.filter((h): h is NonNullable<typeof h> => !!h && picked.has(h.address));
  });
  const singleAddr = () => (props.selectedHosts().length === 1 ? props.selectedHosts()[0] : undefined);
  const host = createMemo(() => {
    const addr = singleAddr();
    return addr ? campaignState().hosts.find(h => h?.address === addr) : undefined;
  });
  const sessions = createMemo(() => {
    const h = host();
    return h ? (sessionsByHostMemo().get(h.address) ?? []) : [];
  });
  const creds = createMemo(() => {
    const h = host();
    return h ? (credsByHostMemo().get(h.address) ?? []) : [];
  });
  const services = createMemo(() => {
    const h = host();
    return h ? (servicesByHostMemo().get(h.address) ?? []).filter(serviceOpen) : [];
  });
  const multiStats = createMemo(() => {
    let liveSessions = 0;
    let withCreds = 0;
    let openPorts = 0;
    const sessMap = sessionsByHostMemo();
    const credMap = credsByHostMemo();
    const svcMap = servicesByHostMemo();
    for (const h of multiHosts()) {
      liveSessions += sessMap.get(h.address)?.length ?? 0;
      if ((credMap.get(h.address)?.length ?? 0) > 0) withCreds++;
      for (const s of svcMap.get(h.address) ?? []) {
        if (serviceOpen(s)) openPorts++;
      }
    }
    return { liveSessions, withCreds, openPorts };
  });

  return (
    <div class="ins">
      <Show when={multiHosts().length > 1} fallback={
      <Show when={host()} fallback={
        <div class="ins-empty">
          <div class="empty-symbol"><i aria-hidden="true" class="ph ph-target"></i></div>
          <h2>No host selected</h2>
          <p>Click a host to inspect it, or Shift-click / Shift-drag on the map to select a group.</p>
        </div>
      }>
        {(h) => {
          const family = () => detectOsFamily(h());
          const state = () => sessions().length > 0 ? "access" : creds().length > 0 ? "login" : "idle";
          return <>
          <div class="ihost">
            <OsIconBadge family={family()} state={state()} size={40} />
            <div class="ihost-meta">
              <div class="iname">{h().name || h().address}</div>
              <div class="ios">
                {[h().osName, h().osFlavor, h().osVersion].filter(Boolean).join(" ") || "unknown os"}
              </div>
            </div>
            <button class="ibtn" title={`Copy ${h().address}`} aria-label="Copy host address"
              onClick={() => copyWithFeedback(h().address)}>
              <i aria-hidden="true" class="ph ph-copy"></i>
            </button>
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

          <div class="ins-actions">
            <Show when={props.onScan}>
              <button class="abtn" onClick={() => props.onScan!(h().address)} title="Scan services on this host">
                <i aria-hidden="true" class="ph ph-wifi-high"></i>Scan
              </button>
            </Show>
            <Show when={props.onFindAttacks}>
              <button class="abtn" onClick={() => props.onFindAttacks!(h().address)} title="Find matching exploits for this host">
                <i aria-hidden="true" class="ph ph-lightning"></i>Attacks
              </button>
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
                    <button class="abtn" onClick={() => props.onLogin([h().address])}>
                      <i aria-hidden="true" class="ph ph-key"></i>Login / Lateral…
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
        </>;}}
      </Show>
      }>
        {(() => {
          const addrs = () => multiHosts().map(h => h.address);
          const targets = () => formatRhosts(addrs());
          return <>
            <div class="ihost">
              <div class="ihost-meta">
                <div class="iname">{multiHosts().length} hosts selected</div>
                <div class="ios">Bulk actions prefill RHOSTS with this group</div>
              </div>
              <button class="ibtn" title="Copy RHOSTS list" aria-label="Copy selected addresses"
                onClick={() => copyWithFeedback(targets())}>
                <i aria-hidden="true" class="ph ph-copy"></i>
              </button>
              <button class="ibtn" title="Clear selection" aria-label="Clear selection"
                onClick={props.onClearSelection}>
                <i aria-hidden="true" class="ph ph-x"></i>
              </button>
            </div>

            <div class="badges">
              <span class="bdg gry">{multiStats().openPorts} open port{multiStats().openPorts === 1 ? "" : "s"}</span>
              <Show when={multiStats().liveSessions > 0}>
                <span class="bdg grn">{multiStats().liveSessions} live session{multiStats().liveSessions === 1 ? "" : "s"}</span>
              </Show>
              <Show when={multiStats().withCreds > 0}>
                <span class="bdg amb">{multiStats().withCreds} with creds</span>
              </Show>
            </div>

            <div class="ins-actions">
              <Show when={props.onScan}>
                <button class="abtn" onClick={() => props.onScan!(targets())}>
                  <i aria-hidden="true" class="ph ph-wifi-high"></i>Scan
                </button>
              </Show>
              <button class="abtn" onClick={() => props.onLogin(addrs())}>
                <i aria-hidden="true" class="ph ph-key"></i>Lateral
              </button>
            </div>
            <div class="ins-actions" style="margin-top:6px">
              <Show when={props.onBrute}>
                <button class="abtn" onClick={() => props.onBrute!(addrs())}>
                  <i aria-hidden="true" class="ph ph-password"></i>Brute
                </button>
              </Show>
              <Show when={props.onHailMary}>
                <button class="abtn" onClick={() => props.onHailMary!(addrs())}>
                  <i aria-hidden="true" class="ph ph-fire"></i>Hail Mary
                </button>
              </Show>
            </div>

            <div class="isec">
              <div class="ilabel">Selected targets</div>
              <div class="picklist" style="max-height:320px; margin-top:6px">
                <For each={multiHosts()}>{(h) => (
                  <button class="pickrow" style="text-align:left; width:100%" onClick={() => props.onSelectOne(h.address)}>
                    <span class="addr">{h.address}</span>
                    <span class="fdesc" style="margin-left:auto">{h.name || h.osName || ""}</span>
                  </button>
                )}</For>
              </div>
            </div>
          </>;
        })()}
      </Show>
    </div>
  );
}
