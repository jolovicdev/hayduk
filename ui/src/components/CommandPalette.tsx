import { For, Show, createEffect, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { campaignState } from "../stores/store";

export interface PaletteItem {
  id: string;
  icon: string;
  label: string;
  sub?: string;
  kind: string;
  run: () => void;
}

const MODULE_BUCKETS: readonly { prop: "exploits" | "auxiliary" | "post" | "payloads" | "encoders" | "nops" | "evasion"; type: string }[] = [
  { prop: "exploits", type: "exploit" },
  { prop: "auxiliary", type: "auxiliary" },
  { prop: "post", type: "post" },
  { prop: "payloads", type: "payload" },
  { prop: "encoders", type: "encoder" },
  { prop: "evasion", type: "evasion" },
  { prop: "nops", type: "nop" },
];

export function CommandPalette(props: {
  connected: boolean;
  onClose: () => void;
  onAction: (action: string) => void;
  onSelectHost: (addr: string) => void;
  onInteract: (sid: string) => void;
  onLaunchModule: (type: string, path: string) => void;
}) {
  const [query, setQuery] = createSignal("");
  const [idx, setIdx] = createSignal(0);
  let inputEl!: HTMLInputElement;

  onMount(() => {
    inputEl?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        props.onClose();
      }
    };
    document.addEventListener("keydown", onKey, true);
    onCleanup(() => document.removeEventListener("keydown", onKey, true));
  });

  const baseItems = createMemo<PaletteItem[]>(() => {
    const state = campaignState();
    const out: PaletteItem[] = [];

    if (props.connected) {
      out.push(
        { id: "act-discover", icon: "crosshair", label: "Discover hosts…", sub: "Campaign", kind: "ACTION", run: () => props.onAction("discover") },
        { id: "act-scan", icon: "wifi-high", label: "Scan services…", sub: "Campaign", kind: "ACTION", run: () => props.onAction("act:scan") },
        { id: "act-find", icon: "lightning", label: "Find attacks…", sub: "Campaign", kind: "ACTION", run: () => props.onAction("find") },
        { id: "act-brute", icon: "key", label: "Brute force logins…", sub: "Campaign", kind: "ACTION", run: () => props.onAction("brute") },
        { id: "act-hail", icon: "fire", label: "Hail Mary…", sub: "Campaign", kind: "ACTION", run: () => props.onAction("hail") },
        { id: "act-report", icon: "download-simple", label: "Export HTML report", sub: "File", kind: "ACTION", run: () => props.onAction("report") },
      );
    }

    out.push(
      { id: "view-topo", icon: "graph", label: "Topology map", sub: "1", kind: "VIEW", run: () => props.onAction("stage:topo") },
      { id: "view-svc", icon: "table", label: "Services table", sub: "2", kind: "VIEW", run: () => props.onAction("stage:svc") },
      { id: "view-hosts", icon: "stack", label: "Hosts table", sub: "3", kind: "VIEW", run: () => props.onAction("stage:hosts") },
      { id: "view-focus", icon: "arrows-out-simple", label: "Toggle map focus mode", kind: "VIEW", run: () => props.onAction("focus") },
      { id: "view-fit", icon: "corners-out", label: "Fit topology to view", sub: "F", kind: "VIEW", run: () => props.onAction("fit") },
      { id: "tab-console", icon: "terminal-window", label: "Framework console", kind: "DOCK", run: () => props.onAction("tab:console") },
      { id: "tab-interact", icon: "terminal", label: "Session interact", kind: "DOCK", run: () => props.onAction("tab:interact") },
      { id: "tab-sessions", icon: "broadcast", label: "Sessions", kind: "DOCK", run: () => props.onAction("tab:sessions") },
      { id: "tab-listeners", icon: "plug", label: "Listeners", kind: "DOCK", run: () => props.onAction("tab:listeners") },
      { id: "tab-jobs", icon: "gear", label: "Background jobs", kind: "DOCK", run: () => props.onAction("tab:jobs") },
      { id: "tab-ops", icon: "pulse", label: "Operations", kind: "DOCK", run: () => props.onAction("tab:ops") },
      { id: "tab-creds", icon: "key", label: "Credentials", kind: "DOCK", run: () => props.onAction("tab:creds") },
      { id: "tab-loot", icon: "archive", label: "Loot", kind: "DOCK", run: () => props.onAction("tab:loot") },
      { id: "tab-events", icon: "pulse", label: "Event log", kind: "DOCK", run: () => props.onAction("tab:events") },
    );

    for (const s of Object.values(state.sessions)) {
      if (!s) continue;
      out.push({
        id: `sess-${s.id}`,
        icon: "broadcast",
        label: `Session ${s.id} · ${s.type}`,
        sub: s.targetHost || s.username || "",
        kind: "SESSION",
        run: () => props.onInteract(s.id),
      });
    }

    for (const h of state.hosts) {
      if (!h) continue;
      out.push({
        id: `host-${h.address}`,
        icon: "target",
        label: h.name ? `${h.name} (${h.address})` : h.address,
        sub: [h.osName, h.osFlavor].filter(Boolean).join(" "),
        kind: "HOST",
        run: () => props.onSelectHost(h.address),
      });
    }

    return out;
  });

  const results = createMemo<PaletteItem[]>(() => {
    const q = query().trim().toLowerCase();
    const terms = q ? q.split(/\s+/) : [];
    const matched = terms.length === 0
      ? baseItems().slice(0, 18)
      : baseItems().filter(item => {
          const hay = `${item.label} ${item.sub ?? ""} ${item.kind}`.toLowerCase();
          return terms.every(t => hay.includes(t));
        });

    if (q.length >= 2 && props.connected) {
      const mods = campaignState().modules;
      if (mods) {
        const modHits: PaletteItem[] = [];
        for (const bucket of MODULE_BUCKETS) {
          const list = mods[bucket.prop];
          if (!list) continue;
          for (const path of list) {
            const full = `${bucket.type}/${path}`.toLowerCase();
            if (terms.every(t => full.includes(t))) {
              modHits.push({
                id: `mod-${bucket.type}-${path}`,
                icon: "rocket-launch",
                label: path,
                sub: bucket.type,
                kind: "MODULE",
                run: () => props.onLaunchModule(bucket.type, path),
              });
              if (modHits.length >= 24) break;
            }
          }
          if (modHits.length >= 24) break;
        }
        return [...matched.slice(0, 14), ...modHits];
      }
    }

    return matched.slice(0, 32);
  });

  createEffect(() => {
    results();
    setIdx(0);
  });

  function execute(item: PaletteItem | undefined) {
    if (!item) return;
    props.onClose();
    item.run();
  }

  function onInputKey(e: KeyboardEvent) {
    const list = results();
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (list.length > 0) setIdx(i => (i + 1) % list.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (list.length > 0) setIdx(i => (i - 1 + list.length) % list.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      execute(list[idx()]);
    }
  }

  return (
    <div class="modalback cmdk-back show" onClick={(e) => { if (e.target === e.currentTarget) props.onClose(); }}>
      <div class="modal cmdk" role="dialog" aria-modal="true" aria-label="Command palette">
        <div class="cmdk-input-wrap">
          <i aria-hidden="true" class="ph ph-magnifying-glass"></i>
          <input
            ref={inputEl}
            value={query()}
            onInput={(e) => setQuery(e.currentTarget.value)}
            onKeyDown={onInputKey}
            placeholder="Jump to host, session, view, action, or module…"
            autocomplete="off"
            spellcheck={false}
          />
          <kbd>ESC</kbd>
        </div>
        <div class="cmdk-list">
          <Show when={results().length > 0} fallback={
            <div class="cmdk-empty">No matching commands, hosts, or modules.</div>
          }>
            <For each={results()}>{(item, i) => (
              <button
                class="cmdk-item"
                classList={{ sel: i() === idx() }}
                aria-current={i() === idx() ? "true" : undefined}
                onMouseEnter={() => setIdx(i())}
                onClick={() => execute(item)}
              >
                <i aria-hidden="true" class={`ph ph-${item.icon}`}></i>
                <span class="cmdk-label">{item.label}</span>
                <Show when={item.sub}><span class="cmdk-sub">{item.sub}</span></Show>
                <span class="cmdk-kind">{item.kind}</span>
              </button>
            )}</For>
          </Show>
        </div>
        <div class="cmdk-foot">
          <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
          <span><kbd>↵</kbd> run</span>
          <span>type 2+ chars to search modules</span>
        </div>
      </div>
    </div>
  );
}
