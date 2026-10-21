import { createSignal } from "solid-js";
import type { EventEntry, EventMsg } from "../protocol/types";
import { ws } from "../ws/singleton";

const [list, setList] = createSignal<EventEntry[]>([]);
const [gap, setGap] = createSignal(false);
let lastSeq = 0;
const CAP = 2000;

export function appendEvent(m: EventMsg) {
  if (lastSeq && m.seq !== lastSeq + 1) setGap(true);
  lastSeq = m.seq;
  setList(prev => {
    const entry: EventEntry = {
      seq: m.seq,
      // the server stamp survives reconnects and clock skew between
      // operators; the receive time is only a fallback for old servers
      time: m.time || new Date().toISOString(),
      level: m.level, text: m.text,
      ...(m.operator ? { operator: m.operator } : {}),
    };
    // Newest first: the view no longer copies and reverses the whole log for
    // every arriving event.
    const next = [entry, ...prev];
    return next.length > CAP ? next.slice(0, CAP) : next;
  });
}

export function syncEvents(entries: EventEntry[] | undefined) {
  lastSeq = entries?.at(-1)?.seq ?? 0;
  setGap(false);
  // The wire snapshot is oldest-first; keep the view's newest-first order.
  setList((entries ?? []).slice().reverse());
}

ws.on("event", appendEvent);
ws.on("snapshot", (m) => syncEvents(m.state?.events));

export { list as events, gap as eventGap };
