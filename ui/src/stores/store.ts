import { createMemo, createRoot, createSignal } from "solid-js";
import type { CampaignState } from "../protocol/types";
import { applyResource, credsByHost, emptyState, normalizeState, sessionsByHost } from "./campaign";
import { currentOperator } from "../ws/client";
import { ws } from "../ws/singleton";

const [state, setState] = createSignal<CampaignState>(emptyState());
const [wsStatus, setWsStatus] = createSignal<"connecting" | "open" | "closed">("connecting");
const [team, setTeam] = createSignal(false);
const [serverVersion, setServerVersion] = createSignal("");
const [protoMismatch, setProtoMismatch] = createSignal(false);

createRoot(() => {
  ws.on("hello", (m) => {
    setTeam(!!m.team);
    setServerVersion(typeof m.version === "string" ? m.version : "");
    // the server learns presence from commands; a reconnect (or server
    // restart) starts with a blank operator list, so announce again
    if (m.team && currentOperator()) void ws.command("operator.join").catch(() => {});
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

const sessionsByHostMemo = createRoot(() => createMemo(() => sessionsByHost(state())));
const credsByHostMemo = createRoot(() => createMemo(() => credsByHost(state())));

export { state as campaignState, wsStatus, team, serverVersion, protoMismatch, sessionsByHostMemo, credsByHostMemo };
