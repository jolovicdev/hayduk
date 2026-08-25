import { describe, expect, it, vi } from "vitest";
import {
  AUTOROUTE_MODULE,
  autorouteAdd,
  autorouteAutoadd,
  autorouteRemove,
  isIPv4,
  parseRouteTarget,
  prefixToNetmask,
  removeRouteItems,
} from "./pivot";

describe("prefixToNetmask", () => {
  it("converts prefix bits to dotted masks", () => {
    expect(prefixToNetmask(24)).toBe("255.255.255.0");
    expect(prefixToNetmask(16)).toBe("255.0.0.0".replace("255.0.0.0", "255.255.0.0"));
    expect(prefixToNetmask(8)).toBe("255.0.0.0");
    expect(prefixToNetmask(0)).toBe("0.0.0.0");
    expect(prefixToNetmask(32)).toBe("255.255.255.255");
    expect(prefixToNetmask(30)).toBe("255.255.255.252");
    expect(prefixToNetmask(23)).toBe("255.255.254.0");
  });
  it("rejects out-of-range or fractional bits", () => {
    expect(prefixToNetmask(33)).toBeUndefined();
    expect(prefixToNetmask(-1)).toBeUndefined();
    expect(prefixToNetmask(8.5)).toBeUndefined();
    expect(prefixToNetmask(NaN)).toBeUndefined();
  });
});

describe("parseRouteTarget", () => {
  it("splits a polled subnet into address and dotted netmask", () => {
    expect(parseRouteTarget("10.99.0.0/24")).toEqual({ subnet: "10.99.0.0", netmask: "255.255.255.0" });
    expect(parseRouteTarget("192.168.0.0/16")).toEqual({ subnet: "192.168.0.0", netmask: "255.255.0.0" });
  });
  it("keeps the subnet as-is when no prefix is known", () => {
    expect(parseRouteTarget("10.99.0.0")).toEqual({ subnet: "10.99.0.0" });
    expect(parseRouteTarget("dead:beef::/64")).toEqual({ subnet: "dead:beef::/64" });
  });
});

describe("isIPv4", () => {
  it("accepts plain dotted quads", () => {
    expect(isIPv4("10.0.0.1")).toBe(true);
    expect(isIPv4("192.168.13.254")).toBe(true);
  });
  it("rejects malformed input", () => {
    expect(isIPv4("10.0.0")).toBe(false);
    expect(isIPv4("999.1.1.1")).toBe(false);
    expect(isIPv4("10.0.0.1/24")).toBe(false);
    expect(isIPv4("host.local")).toBe(false);
    expect(isIPv4("")).toBe(false);
  });
});

describe("autoroute module options", () => {
  it("autoadd binds the session only", () => {
    expect(autorouteAutoadd("3")).toEqual({ SESSION: "3", CMD: "autoadd" });
  });
  it("manual add carries subnet and dotted netmask", () => {
    expect(autorouteAdd("3", "10.13.0.0", 24)).toEqual({
      SESSION: "3", CMD: "add", SUBNET: "10.13.0.0", NETMASK: "255.255.255.0",
    });
  });
  it("remove deletes the exact route through its owning session", () => {
    expect(autorouteRemove("2", { subnet: "10.99.0.0", netmask: "255.255.255.0" })).toEqual({
      SESSION: "2", CMD: "delete", SUBNET: "10.99.0.0", NETMASK: "255.255.255.0",
    });
  });
  it("remove omits the netmask when the prefix was not parseable", () => {
    expect(autorouteRemove("2", { subnet: "10.99.0.0" })).toEqual({
      SESSION: "2", CMD: "delete", SUBNET: "10.99.0.0",
    });
  });
  it("runs through the autoroute post module", () => {
    expect(AUTOROUTE_MODULE).toBe("multi/manage/autoroute");
  });
});

describe("removeRouteItems", () => {
  it("offers one removal per route", () => {
    const onRemove = vi.fn();
    const items = removeRouteItems(
      [{ subnet: "10.99.0.0/24", sessionId: "2" }, { subnet: "10.99.0.0/24", sessionId: "5" }],
      onRemove,
    );
    expect(items[0]).toMatchObject({ head: "10.99.0.0/24" });
    const labels = items.filter(it => it.label).map(it => it.label);
    expect(labels).toEqual(["Remove route via session 2", "Remove route via session 5"]);
    items.filter(it => it.fn)[1]!.fn!();
    expect(onRemove).toHaveBeenCalledWith({ subnet: "10.99.0.0/24", sessionId: "5" });
  });
  it("marks removals as danger actions", () => {
    const items = removeRouteItems([{ subnet: "10.0.0.0/8", sessionId: "1" }], vi.fn());
    expect(items[1]).toMatchObject({ danger: true });
  });
  it("skips routes without a subnet or session", () => {
    expect(removeRouteItems([{ subnet: "", sessionId: "1" }, { subnet: "a/24", sessionId: "" }], vi.fn())).toEqual([]);
  });
});
