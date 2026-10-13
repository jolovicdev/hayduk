import { createSignal } from "solid-js";
import { ws } from "../ws/singleton";

const CAP = 128_000;

const [sid, setSid] = createSignal("");
const [tabs, setTabs] = createSignal<string[]>(loadTabs());
const [outputs, setOutputs] = createSignal<Record<string, string>>({});

function loadTabs(): string[] {
  try {
    const raw = localStorage.getItem("hayduk.session-tabs");
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === "string") : [];
  } catch {
    return [];
  }
}

function saveTabs() {
  try {
    localStorage.setItem("hayduk.session-tabs", JSON.stringify(tabs()));
  } catch {
    // storage may be unavailable; tabs still work for this page session
  }
}

function appendChunk(target: string, data: string) {
  setOutputs(prev => {
    const next = (prev[target] ?? "") + data;
    return { ...prev, [target]: next.length > CAP ? next.slice(next.length - CAP) : next };
  });
}

ws.on("sessionOutput", (m) => {
  if (!m?.sid) return;
  appendChunk(m.sid, m.data ?? "");
});
ws.on("resource", (m) => {
  if (m.resource === "interact") {
    const nextSid: string = m.interact?.sid ?? "";
    if (nextSid) {
      setOutputs(prev => ({ ...prev, [nextSid]: m.interact?.output ?? "" }));
      if (!tabs().includes(nextSid)) {
        setTabs(prev => [...prev, nextSid]);
        saveTabs();
      }
      setSid(nextSid);
    }
  }
});
ws.on("snapshot", (m) => {
  const snapSid: string = m.state?.interact?.sid ?? "";
  if (snapSid) {
    setOutputs(prev => ({ ...prev, [snapSid]: m.state?.interact?.output ?? "" }));
    if (!tabs().includes(snapSid)) {
      setTabs(prev => [...prev, snapSid]);
      saveTabs();
    }
    setSid(snapSid);
  }
});

export async function attach(sessionId: string) {
  await ws.command("session.attach", { sid: sessionId });
  if (!tabs().includes(sessionId)) {
    setTabs(prev => [...prev, sessionId]);
    saveTabs();
  }
  setSid(sessionId);
}

export async function detach(targetSid?: string) {
  const target = targetSid ?? sid();
  if (!target) return;
  await ws.command("session.detach", { sid: target });
  setTabs(prev => prev.filter(t => t !== target));
  saveTabs();
  setOutputs(prev => {
    const next = { ...prev };
    delete next[target];
    return next;
  });
  if (sid() === target) {
    setSid(tabs().at(-1) ?? "");
  }
}

export function activateTab(sessionId: string) {
  if (!tabs().includes(sessionId)) return;
  // switching tabs re-attaches so the engine replays that session's buffer
  void attach(sessionId).catch(() => setSid(sessionId));
}

export function write(data: string, targetSid?: string) {
  // targetSid binds the write to the session the draft was typed for: an
  // attachment that moved underneath the draft fails server-side instead of
  // silently retargeting the command at the new session.
  return ws.command("session.write", { sid: targetSid ?? sid(), data });
}

export const interactSID = sid;
export const interactTabs = tabs;
export const interactOutput = () => outputs()[sid()] ?? "";
export const interactOutputFor = (target: string) => outputs()[target] ?? "";
