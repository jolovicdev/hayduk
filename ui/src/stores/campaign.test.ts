import { describe, expect, it } from "vitest";
import { applyResource, credsByHost, emptyState, sessionsByHost } from "./campaign";

describe("campaign store", () => {
  it("applies resource updates", () => {
    const s = emptyState();
    applyResource(s, { type: "resource", resource: "sessions", sessions: { "1": { id: "1", type: "shell" } as any } });
    expect(s.sessions["1"]?.type).toBe("shell");
  });

  it("derives sessions-by-host index", () => {
    const s = emptyState();
    s.sessions = {
      "1": { id: "1", targetHost: "10.0.0.5" } as any,
      "2": { id: "2", targetHost: "10.0.0.5" } as any,
      "3": { id: "3", targetHost: "10.0.0.9" } as any,
    };
    const byHost = sessionsByHost(s);
    expect(byHost.get("10.0.0.5")?.length).toBe(2);
    expect(byHost.get("10.0.0.9")?.length).toBe(1);
    expect(byHost.get("10.0.0.1")?.length).toBeUndefined();
  });

  it("derives creds-by-host index", () => {
    const s = emptyState();
    s.creds = [
      { host: "10.0.0.5", user: "a", pass: "x", type: "password" } as any,
      { host: "10.0.0.5", user: "b", pass: "y", type: "password" } as any,
      { host: "", user: "c", pass: "z", type: "password" } as any,
    ];
    const byHost = credsByHost(s);
    expect(byHost.get("10.0.0.5")?.length).toBe(2);
    expect(byHost.has("")).toBe(false);
  });

  it("falls back to sessionHost when targetHost is empty", () => {
    const s = emptyState();
    s.sessions = { "7": { id: "7", targetHost: "", sessionHost: "10.1.1.1" } as any };
    const byHost = sessionsByHost(s);
    expect(byHost.get("10.1.1.1")?.length).toBe(1);
  });
});

describe("normalizeState", () => {
  it("turns null collections into empty ones", async () => {
    const { normalizeState } = await import("./campaign");
    const s = normalizeState({
      connection: { status: "disconnected" },
      hosts: null, services: null, creds: null, loot: null, events: null,
      sessions: null, jobs: null,
    } as any);
    expect(s.hosts).toEqual([]);
    expect(s.creds).toEqual([]);
    expect(s.sessions).toEqual({});
    expect(Array.isArray(s.loot)).toBe(true);
  });

  it("keeps real values untouched", async () => {
    const { normalizeState } = await import("./campaign");
    const s = normalizeState({
      connection: { status: "connected" },
      hosts: [{ address: "10.0.0.1" }],
      sessions: { "1": { id: "1" } },
    } as any);
    expect(s.hosts).toHaveLength(1);
    expect(s.sessions["1"]?.id).toBe("1");
  });
});
