import { describe, expect, it } from "vitest";
import { loadConnectDefaults, parsePort } from "./connectForm";

describe("parsePort", () => {
  it("accepts real ports", () => {
    expect(parsePort("55553")).toBe(55553);
    expect(parsePort("1")).toBe(1);
    expect(parsePort("65535")).toBe(65535);
  });

  it("rejects garbage instead of producing NaN or out-of-range ports", () => {
    expect(parsePort("")).toBeUndefined();
    expect(parsePort("abc")).toBeUndefined();
    expect(parsePort("0")).toBeUndefined();
    expect(parsePort("65536")).toBeUndefined();
    expect(parsePort("-1")).toBeUndefined();
    expect(parsePort("4444.5")).toBeUndefined();
  });
});

describe("loadConnectDefaults", () => {
  it("merges stored values over the defaults with correct types", () => {
    const storage = {
      getItem: (k: string) =>
        ({ "hayduk.host": "10.0.0.9", "hayduk.port": "55554", "hayduk.ssl": "true" } as Record<string, string>)[k] ?? null,
    };
    expect(loadConnectDefaults(storage)).toEqual({ host: "10.0.0.9", port: 55554, ssl: true, username: "msf" });
  });

  it("corrupt stored values fall back to the defaults", () => {
    const storage = { getItem: () => "garbage" };
    const d = loadConnectDefaults(storage);
    expect(d.port).toBe(55553);
    expect(d.ssl).toBe(false);
  });
});
