import { describe, expect, it } from "vitest";
import { WEB_DELIVERY_PLATFORMS, defaultPlatform, webDeliveryReady } from "./webDelivery";

describe("web delivery platforms", () => {
  it("defaults every platform to a payload its interpreter can run", () => {
    expect(defaultPlatform("python").payload).toBe("python/meterpreter/reverse_tcp");
    expect(defaultPlatform("php").payload).toBe("php/meterpreter/reverse_tcp");
    expect(defaultPlatform("psh").payload).toContain("windows/");
    expect(defaultPlatform("linux").payload).toContain("linux/");
    expect(defaultPlatform("osx").payload).toContain("osx/");
  });
  it("falls back to the first platform for unknown values", () => {
    expect(defaultPlatform("nope").value).toBe(WEB_DELIVERY_PLATFORMS[0]!.value);
  });
});

describe("webDeliveryReady", () => {
  const base = { payload: "python/meterpreter/reverse_tcp", lhost: "10.0.0.1", lport: "4444", srvPort: "8080" };
  it("accepts a complete form", () => {
    expect(webDeliveryReady(base)).toBe(true);
  });
  it("rejects missing payload, lhost, or bad ports", () => {
    expect(webDeliveryReady({ ...base, payload: "" })).toBe(false);
    expect(webDeliveryReady({ ...base, lhost: "  " })).toBe(false);
    expect(webDeliveryReady({ ...base, lport: "0" })).toBe(false);
    expect(webDeliveryReady({ ...base, srvPort: "abc" })).toBe(false);
  });
});
