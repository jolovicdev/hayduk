import { createSignal } from "solid-js";
import type { EventEntry, EventMsg } from "../protocol/types";
import { ws } from "../ws/singleton";

const [list, setList] = createSignal<EventEntry[]>([]);
const [gap, setGap] = createSignal(false);
// chat that landed while the Events tab was closed; the tab badge shows it
const [unreadChat, setUnreadChat] = createSignal(0);
let lastSeq = 0;
const CAP = 2000;

export function appendEvent(m: EventMsg) {
  if (lastSeq && m.seq !== lastSeq + 1) setGap(true);
  lastSeq = m.seq;
  if (m.level === "chat") setUnreadChat(n => n + 1);
  setList(prev => {
    const next = [...prev, {
      seq: m.seq,
      // the server stamp survives reconnects and clock skew between
      // operators; the receive time is only a fallback for old servers
      time: m.time || new Date().toISOString(),
      level: m.level, text: m.text,
      ...(m.operator ? { operator: m.operator } : {}),
    }];
    return next.length > CAP ? next.slice(next.length - CAP) : next;
  });
}

export function syncEvents(entries: EventEntry[] | undefined) {
  lastSeq = entries?.at(-1)?.seq ?? 0;
  setGap(false);
  setList(entries ?? []);
}

export function markChatRead() {
  setUnreadChat(0);
}

ws.on("event", appendEvent);
ws.on("snapshot", (m) => syncEvents(m.state?.events));

export { list as events, gap as eventGap, unreadChat };
