import type { CampaignState, CredState, SessionState } from "../protocol/types";

export function emptyState(): CampaignState {
  return {
    connection: { status: "disconnected", host: "", port: 0, ssl: false, username: "", msfVersion: "", workspace: "" },
    hosts: [], services: [], sessions: {}, jobs: {}, routes: [], creds: [], loot: [],
    moduleRanks: {},
    operators: [],
    events: [],
  };
}

// normalizeState guards the snapshot spread: a server that omits or nulls
// collection fields must never produce null arrays in the store
export function normalizeState(raw: any): CampaignState {
  const base = emptyState();
  return {
    ...base,
    ...raw,
    hosts: raw?.hosts ?? [],
    services: raw?.services ?? [],
    sessions: raw?.sessions ?? {},
    jobs: raw?.jobs ?? {},
    routes: raw?.routes ?? [],
    creds: raw?.creds ?? [],
    loot: raw?.loot ?? [],
    moduleRanks: raw?.moduleRanks ?? {},
    operators: raw?.operators ?? [],
    events: raw?.events ?? [],
  };
}

export function applyResource(s: CampaignState, msg: any) {
  switch (msg.resource) {
    case "connection": s.connection = msg.connection; break;
    case "hosts": s.hosts = msg.hosts ?? []; break;
    case "services": s.services = msg.services ?? []; break;
    case "sessions": s.sessions = msg.sessions ?? {}; break;
    case "jobs": s.jobs = msg.jobs ?? {}; break;
    case "routes": s.routes = msg.routes ?? []; break;
    case "creds": s.creds = msg.creds ?? []; break;
    case "loot": s.loot = msg.loot ?? []; break;
    case "modules": s.modules = msg.modules; break;
    case "moduleRanks": s.moduleRanks = { ...(s.moduleRanks ?? {}), ...(msg.moduleRanks ?? {}) }; break;
    case "operators": s.operators = msg.operators ?? []; break;
    case "console": s.console = msg.console; break;
    case "interact": s.interact = msg.interact; break;
    default: console.warn("unknown resource", msg.resource);
  }
}

export function sessionsByHost(s: CampaignState): Map<string, SessionState[]> {
  const m = new Map<string, SessionState[]>();
  for (const sess of Object.values(s.sessions)) {
    if (!sess) continue;
    const host = sess.targetHost || sess.sessionHost;
    if (!host) continue;
    const list = m.get(host) ?? [];
    list.push(sess);
    m.set(host, list);
  }
  return m;
}

export function credsByHost(s: CampaignState): Map<string, CredState[]> {
  const m = new Map<string, CredState[]>();
  for (const c of s.creds) {
    if (!c || !c.host) continue;
    const list = m.get(c.host) ?? [];
    list.push(c);
    m.set(c.host, list);
  }
  return m;
}


