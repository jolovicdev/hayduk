import { createMemo, createRoot, createSignal } from "solid-js";
import type { CampaignState } from "../protocol/types";
import { applyResource, credsByHost, emptyState, normalizeState, sessionsByHost } from "./campaign";
import { currentOperator, setOperator } from "../ws/client";
import { ws } from "../ws/singleton";

const [state, setState] = createSignal<CampaignState>(emptyState());
const [wsStatus, setWsStatus] = createSignal<"connecting" | "open" | "closed">("connecting");
const [team, setTeam] = createSignal(false);
const [boundIdentity, setBoundIdentity] = createSignal("");
const [serverVersion, setServerVersion] = createSignal("");
const [protoMismatch, setProtoMismatch] = createSignal(false);

createRoot(() => {
  ws.on("hello", (m) => {
    setTeam(!!m.team);
    setServerVersion(typeof m.version === "string" ? m.version : "");
    const identity = typeof m.identity === "string" ? m.identity : "";
    setBoundIdentity(identity);
    if (identity) {
      setOperator(identity);
    } else if (m.team && currentOperator()) {
      // the server learns presence from commands; a reconnect (or server
      // restart) starts with a blank operator list, so announce again
      void ws.command("operator.join").catch(() => {});
    }
  });
  ws.on("snapshot", (m) => setState(normalizeState(m.state)));
  ws.on("resource", (m) => setState(prev => {
    const next = { ...prev };
    applyResource(next, m);
    return next;
  }));
  ws.on("open", () => setWsStatus("open"));
  ws.on("closed", () => setWsStatus("closed"));
  ws.on("proto_mismatch", () => setProtoMismatch(true));
});

const sessionsSlice = createRoot(() => createMemo(() => state().sessions));
const credsSlice = createRoot(() => createMemo(() => state().creds));
const servicesSlice = createRoot(() => createMemo(() => state().services));
const moduleRanksMemo = createRoot(() => createMemo(() => state().moduleRanks ?? {}));

const sessionsByHostMemo = createRoot(() => createMemo(() =>
  sessionsByHost({ sessions: sessionsSlice() } as CampaignState)));
const credsByHostMemo = createRoot(() => createMemo(() =>
  credsByHost({ creds: credsSlice() } as CampaignState)));
const servicesByHostMemo = createRoot(() => createMemo(() => {
  const map = new Map<string, NonNullable<CampaignState["services"][number]>[]>();
  for (const s of servicesSlice()) {
    if (!s) continue;
    const list = map.get(s.host);
    if (list) list.push(s);
    else map.set(s.host, [s]);
  }
  return map;
}));

export {
  state as campaignState, wsStatus, team, boundIdentity, serverVersion, protoMismatch,
  sessionsByHostMemo, credsByHostMemo, servicesByHostMemo, moduleRanksMemo,
};
