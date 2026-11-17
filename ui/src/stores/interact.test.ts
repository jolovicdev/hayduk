import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// a socket that answers every command ok, so store flows relying on
// command resolution (attach, detach) run to completion
class FakeSocket {
  static instances: FakeSocket[] = [];
  readyState = 0;
  sent: any[] = [];
  handlers: Record<string, ((ev: any) => void)[]> = {};
  send(data: string) {
    const m = JSON.parse(data);
    this.sent.push(m);
    queueMicrotask(() =>
      this.handlers["message"]?.forEach(h =>
        h({ data: JSON.stringify({ type: "response", id: m.id, ok: true }) })));
  }
  close() {}
  open() {
    this.readyState = 1;
    this.handlers["open"]?.forEach(h => h({}));
  }
  message(data: any) {
    this.handlers["message"]?.forEach(h => h({ data: JSON.stringify(data) }));
  }
  addEventListener(kind: string, h: (ev: any) => void) {
    (this.handlers[kind] ??= []).push(h);
  }
  constructor() {
    FakeSocket.instances.push(this);
  }
}

const flush = () => new Promise(r => setTimeout(r, 0));

async function loadInteract() {
  return await import("./interact");
}

describe("interact store team views", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", FakeSocket as any);
    vi.stubGlobal("location", { protocol: "http:", host: "localhost" });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("scopes the active session to this operator's interact updates", async () => {
    const store = await loadInteract();
    const sock = FakeSocket.instances[0]!;
    sock.open();
    sock.message({ type: "resource", resource: "interact", operator: "dana", interact: { sid: "1", output: "one" } });
    sock.message({ type: "resource", resource: "interact", operator: "dana", interact: { sid: "2", output: "two" } });
    expect(store.interactSID()).toBe("2");
    expect(store.interactTabs()).toEqual(["1", "2"]);
    expect(store.interactOutputFor("1")).toBe("one");
    expect(store.interactOutput()).toBe("two");
  });

  it("detach falls back to the last tab and re-attaches it", async () => {
    const store = await loadInteract();
    const sock = FakeSocket.instances[0]!;
    sock.open();
    sock.message({ type: "resource", resource: "interact", interact: { sid: "1", output: "one" } });
    sock.message({ type: "resource", resource: "interact", interact: { sid: "2", output: "two" } });
    sock.sent.length = 0;

    await store.detach("2"); // close the active tab
    await flush();
    expect(store.interactSID()).toBe("1"); // fell back to the remaining tab
    expect(store.interactTabs()).toEqual(["1"]);
    const methods = sock.sent.map(m => `${m.method}:${m.params?.sid ?? ""}`);
    expect(methods).toContain("session.detach:2");
    expect(methods).toContain("session.attach:1"); // engine follows the visible tab
  });

  it("resetInteract clears every local view on an identity switch", async () => {
    const store = await loadInteract();
    const sock = FakeSocket.instances[0]!;
    sock.open();
    sock.message({ type: "resource", resource: "interact", interact: { sid: "1", output: "one" } });
    expect(store.interactTabs()).toHaveLength(1);
    store.resetInteract();
    expect(store.interactSID()).toBe("");
    expect(store.interactTabs()).toEqual([]);
    expect(store.interactOutputFor("1")).toBe("");
  });
});
