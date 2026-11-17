import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// the stores bind to the ws singleton at import time, so the socket stub
// must exist before the dynamic import below
class FakeSocket {
  readyState = 0;
  handlers: Record<string, ((ev: any) => void)[]> = {};
  send() {}
  close() {}
  addEventListener(kind: string, h: (ev: any) => void) {
    (this.handlers[kind] ??= []).push(h);
  }
}

async function loadEvents() {
  return await import("./events");
}

describe("events store team chat", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", FakeSocket as any);
    vi.stubGlobal("location", { protocol: "http:", host: "localhost" });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("counts chat that lands while unread and clears on markChatRead", async () => {
    const { appendEvent, markChatRead, unreadChat, events } = await loadEvents();
    expect(unreadChat()).toBe(0);
    appendEvent({ type: "event", seq: 1, time: "", level: "info", text: "session 1 opened" });
    expect(unreadChat()).toBe(0); // plain events are not chat
    appendEvent({ type: "event", seq: 2, time: "", level: "chat", text: "pivot up", operator: "dana" });
    appendEvent({ type: "event", seq: 3, time: "", level: "chat", text: "nice", operator: "mile" });
    expect(unreadChat()).toBe(2);
    expect(events().at(-1)?.operator).toBe("mile");
    markChatRead();
    expect(unreadChat()).toBe(0);
  });

  it("syncEvents replaces the log from a snapshot", async () => {
    const { appendEvent, syncEvents, events, eventGap } = await loadEvents();
    appendEvent({ type: "event", seq: 5, time: "", level: "info", text: "old" });
    syncEvents([{ seq: 9, time: "", level: "chat", text: "fresh", operator: "dana" }]);
    expect(events()).toHaveLength(1);
    expect(events()[0]!.text).toBe("fresh");
    expect(eventGap()).toBe(false);
  });
});
