import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createWS, wsURL } from "./client";

class FakeSocket {
  static instances: FakeSocket[] = [];
  url: string;
  readyState = 0; // 0 connecting, 1 open, 3 closed, like the real socket
  sent: string[] = [];
  handlers: Record<string, ((ev: any) => void)[]> = {};
  send(data: string) {
    if (this.readyState !== 1) throw new Error("InvalidStateError: still in CONNECTING state");
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
    this.handlers["close"]?.forEach(h => h({}));
  }
  open() {
    this.readyState = 1;
    this.handlers["open"]?.forEach(h => h({}));
  }
  message(data: any) {
    this.handlers["message"]?.forEach(h => h({ data: JSON.stringify(data) }));
  }
  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }
  addEventListener(kind: string, h: (ev: any) => void) {
    (this.handlers[kind] ??= []).push(h);
  }
}

describe("ws client", () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeSocket as any);
    vi.stubGlobal("location", { protocol: "http:", host: "localhost" });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("builds a same-origin /ws url", () => {
    expect(wsURL({ protocol: "http:", host: "127.0.0.1:8080" })).toBe("ws://127.0.0.1:8080/ws");
    expect(wsURL({ protocol: "https:", host: "h.example" })).toBe("wss://h.example/ws");
  });

  it("resolves command responses by id", async () => {
    const c = createWS();
    FakeSocket.instances[0]!.open();
    const p = c.command<string[]>("workspace.list");
    FakeSocket.instances[0]!.message({ type: "response", id: 1, ok: true, data: ["default"] });
    await expect(p).resolves.toEqual(["default"]);
    expect(JSON.parse(FakeSocket.instances[0]!.sent[0]!)).toEqual({
      type: "command", id: 1, method: "workspace.list", params: undefined,
    });
  });

  it("rejects on error responses", async () => {
    const c = createWS();
    FakeSocket.instances[0]!.open();
    const p = c.command("bogus");
    FakeSocket.instances[0]!.message({
      type: "response", id: 1, ok: false, error: { code: "unknown_method", message: "no" },
    });
    await expect(p).rejects.toMatchObject({ code: "unknown_method" });
  });

  it("dispatches non-response messages to handlers", () => {
    const c = createWS();
    const seen: string[] = [];
    c.on("event", (m) => seen.push(m.text));
    FakeSocket.instances[0]!.message({ type: "event", seq: 1, level: "info", text: "hi" });
    expect(seen).toEqual(["hi"]);
  });

  it("ignores unknown message types", () => {
    const c = createWS();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    FakeSocket.instances[0]!.message({ seq: 1, text: "no type" });
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it("reconnects after close with backoff", async () => {
    vi.useFakeTimers();
    createWS();
    const first = FakeSocket.instances[0]!;
    first.open();
    first.close();
    await vi.advanceTimersByTimeAsync(1500);
    expect(FakeSocket.instances.length).toBe(2);
    vi.useRealTimers();
  });

  it("rejects pending commands when the socket closes", async () => {
    vi.useFakeTimers();
    const c = createWS();
    const first = FakeSocket.instances[0]!;
    first.open();
    const p = c.command("workspace.list");
    first.close();
    await expect(p).rejects.toMatchObject({ code: "disconnected" });
    // a late response for the dead id must not double-settle anything
    first.message({ type: "response", id: 1, ok: true, data: [] });
    await vi.advanceTimersByTimeAsync(0);
    vi.useRealTimers();
  });

  it("rejects commands fired before the socket opens", async () => {
    const c = createWS();
    const p = c.command("workspace.list");
    await expect(p).rejects.toMatchObject({ code: "disconnected" });
    expect(FakeSocket.instances[0]!.sent).toEqual([]);
    expect(c.status()).toBe("connecting");
  });

  it("delivers hello messages to hello handlers", () => {
    const c = createWS();
    const hellos: any[] = [];
    c.on("hello", m => hellos.push(m));
    FakeSocket.instances[0]!.message({ type: "hello", proto: 1, version: "9.9.9", team: true });
    expect(hellos).toEqual([{ type: "hello", proto: 1, version: "9.9.9", team: true }]);
  });

  it("stops reconnecting after a protocol mismatch", async () => {
    vi.useFakeTimers();
    const mismatches: any[] = [];
    const c = createWS();
    c.on("proto_mismatch", m => mismatches.push(m));
    const first = FakeSocket.instances[0]!;
    first.message({ type: "hello", proto: 99, version: "x", team: false });
    await vi.advanceTimersByTimeAsync(30_000);
    expect(mismatches).toHaveLength(1);
    expect(mismatches[0]!.proto).toBe(99);
    expect(FakeSocket.instances).toHaveLength(1); // no reconnect loop against an incompatible server
    vi.useRealTimers();
  });

  it("keeps reconnecting for a matching protocol hello", async () => {
    vi.useFakeTimers();
    createWS();
    const first = FakeSocket.instances[0]!;
    first.message({ type: "hello", proto: 1, version: "x", team: false });
    first.close();
    await vi.advanceTimersByTimeAsync(1500);
    expect(FakeSocket.instances).toHaveLength(2);
    vi.useRealTimers();
  });
});
