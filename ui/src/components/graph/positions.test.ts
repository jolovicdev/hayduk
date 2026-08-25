import { describe, expect, it, vi } from "vitest";
import { loadPositions, pruneStale, savePositions } from "./positions";

describe("graph positions", () => {
  it("loads positions keyed by host address", () => {
    const storage = {
      getItem: vi.fn(() => JSON.stringify({
        "10.0.0.1": { x: 120, y: 240 },
        "10.0.0.2": { x: 360, y: 480 },
      })),
    };

    expect(loadPositions(storage)).toEqual(new Map([
      ["10.0.0.1", { x: 120, y: 240 }],
      ["10.0.0.2", { x: 360, y: 480 }],
    ]));
  });

  it("saves positions keyed by host address", () => {
    const storage = { setItem: vi.fn() };

    savePositions(new Map([
      ["10.0.0.1", { x: 120, y: 240 }],
      ["10.0.0.2", { x: 360, y: 480 }],
    ]), storage);

    expect(storage.setItem).toHaveBeenCalledWith(
      "hayduk.topology.positions.v3",
      JSON.stringify({
        "10.0.0.1": { x: 120, y: 240 },
        "10.0.0.2": { x: 360, y: 480 },
      }),
    );
  });

  it("falls back to automatic layout after storage is cleared", () => {
    expect(loadPositions({ getItem: () => null })).toEqual(new Map());
  });

  it("does not throw when storage is restricted", () => {
    const storage = {
      setItem: () => { throw new DOMException("quota", "QuotaExceededError"); },
    };
    expect(() => savePositions(new Map([["10.0.0.1", { x: 1, y: 2 }]]), storage as any)).not.toThrow();
  });

  it("pruneStale drops hosts that left the workspace", () => {
    const positions = new Map([
      ["10.0.0.1", { x: 1, y: 2 }],
      ["10.0.0.2", { x: 3, y: 4 }],
      ["10.0.0.3", { x: 5, y: 6 }],
    ]);
    const pruned = pruneStale(positions, new Set(["10.0.0.2"]));
    expect([...pruned.keys()]).toEqual(["10.0.0.2"]);
    // the original map is untouched
    expect(positions.size).toBe(3);
  });

  it("ignores corrupt positions", () => {
    const storage = {
      getItem: () => JSON.stringify({
        "10.0.0.1": { x: "left", y: 240 },
        "10.0.0.2": { x: 360, y: 480 },
      }),
    };

    expect(loadPositions(storage)).toEqual(new Map([
      ["10.0.0.2", { x: 360, y: 480 }],
    ]));
  });
});
