import { describe, expect, it } from "vitest";
import { friendlyConnectError, loadConnectDefaults, parsePort } from "./connectForm";

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

describe("friendlyConnectError", () => {
  it("translates the Ruby login failure", () => {
    expect(friendlyConnectError("Msf::RPC::Exception: Login Failed")).toEqual({
      primary: "Login failed - msfrpcd rejected the username or password.",
      detail: "Msf::RPC::Exception: Login Failed",
    });
  });

  it("translates refused and timed-out dials", () => {
    expect(friendlyConnectError("dial tcp 127.0.0.1:55553: connect: connection refused").primary)
      .toContain("Connection refused");
    expect(friendlyConnectError("context deadline exceeded").primary)
      .toContain("Timed out");
  });

  it("suggests the SSL toggle on protocol mismatch", () => {
    expect(friendlyConnectError("http: server gave HTTP response to HTTPS client").primary)
      .toContain("SSL");
  });

  it("keeps unknown errors verbatim with no detail line", () => {
    expect(friendlyConnectError("something novel")).toEqual({
      primary: "something novel",
      detail: "",
    });
  });
});
