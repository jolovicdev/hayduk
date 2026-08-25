import { describe, expect, it } from "vitest";
import { GROUP_HEADER, GROUP_INSET, NH, NW, fitLabel, layoutHosts, subnetGroups, subnetKey } from "./layout";
import { osBadge } from "../../views/os";

describe("subnetKey", () => {
  it("groups by first three octets", () => {
    expect(subnetKey("10.10.1.5")).toBe("10.10.1");
    expect(subnetKey("10.10.2.103")).toBe("10.10.2");
  });
  it("handles non-ipv4 as other", () => {
    expect(subnetKey("::1")).toBe("other");
    expect(subnetKey("")).toBe("other");
    expect(subnetKey("hostname")).toBe("other");
  });

  it("rejects malformed numeric forms", () => {
    expect(subnetKey("1e2.10.0.1")).toBe("other"); // exponent
    expect(subnetKey("0x10.10.0.1")).toBe("other"); // hex
    expect(subnetKey(" 10.10.0.1")).toBe("other"); // leading space parses as a number
    expect(subnetKey("10.10.0.+1")).toBe("other");
    expect(subnetKey("10..0.1")).toBe("other");
  });

  it("rejects octets above 255", () => {
    expect(subnetKey("300.10.0.1")).toBe("other");
    expect(subnetKey("10.10.999.1")).toBe("other");
    expect(subnetKey("10.10.0.256")).toBe("other");
    expect(subnetKey("255.255.255.255")).toBe("255.255.255");
  });
});

describe("layoutHosts", () => {
  it("sorts subnets and hosts numerically", () => {
    const pos = layoutHosts(
      [{ address: "10.0.2.10" }, { address: "10.0.1.20" }, { address: "10.0.1.3" }],
      new Map(),
    );
    const keys = [...pos.keys()];
    expect(keys.indexOf("10.0.1.3")).toBeLessThan(keys.indexOf("10.0.1.20"));
    expect(keys.indexOf("10.0.1.20")).toBeLessThan(keys.indexOf("10.0.2.10"));
  });

  it("wraps rows at three hosts", () => {
    const hosts = Array.from({ length: 5 }, (_, i) => ({ address: `10.0.1.${i + 1}` }));
    const pos = layoutHosts(hosts, new Map());
    const second = pos.get("10.0.1.4")!;
    const first = pos.get("10.0.1.1")!;
    expect(second.y).toBeGreaterThan(first.y);
    expect(second.x).toBe(first.x);
  });

  it("keeps sticky overrides", () => {
    const sticky = new Map([["10.0.1.1", { x: 999, y: 999 }]]);
    const pos = layoutHosts([{ address: "10.0.1.1" }], sticky);
    expect(pos.get("10.0.1.1")).toEqual({ x: 999, y: 999 });
  });

  it("places hosts from different subnets in separate rows", () => {
    const pos = layoutHosts([{ address: "10.0.1.1" }, { address: "10.0.2.1" }], new Map());
    expect(pos.get("10.0.2.1")!.y).toBeGreaterThan(pos.get("10.0.1.1")!.y);
  });

  it("nodes never overlap horizontally within a row", () => {
    const hosts = Array.from({ length: 3 }, (_, i) => ({ address: `10.0.1.${i + 1}` }));
    const pos = layoutHosts(hosts, new Map());
    const xs = hosts.map(h => pos.get(h.address)!.x).sort((a, b) => a - b);
    for (let i = 1; i < xs.length; i++) {
      expect(xs[i]! - xs[i - 1]!).toBeGreaterThanOrEqual(NW);
    }
  });
});

describe("subnetGroups", () => {
  it("contains every host without excess empty space", () => {
    const hosts = [{ address: "10.0.1.1" }, { address: "10.0.1.2" }];
    const positions = layoutHosts(hosts, new Map());
    const group = subnetGroups(hosts, positions).get("10.0.1")!;

    expect(group).toEqual({
      x: 0,
      y: 0,
      w: NW * 2 + 26 + GROUP_INSET * 2,
      h: GROUP_HEADER + NH + GROUP_INSET,
      count: 2,
    });
  });

  it("separates adjacent subnet panels", () => {
    const hosts = [{ address: "10.0.1.1" }, { address: "10.0.2.1" }];
    const groups = [...subnetGroups(hosts).values()];
    expect(groups[1]!.y).toBeGreaterThanOrEqual(groups[0]!.y + groups[0]!.h);
  });
});

describe("fitLabel", () => {
  it("keeps short text and truncates long text", () => {
    expect(fitLabel("web01", 10)).toBe("web01");
    expect(fitLabel("database.internal", 10)).toBe("database.…");
  });
});

describe("osBadge", () => {
  it("maps windows families", () => {
    expect(osBadge({ osName: "Microsoft Windows 7 Enterprise" }).key).toBe("win");
    expect(osBadge({ osName: "Windows Server 2019 Standard" }).label).toBe("SRV 2019 STANDARD");
  });
  it("maps linux families", () => {
    expect(osBadge({ osName: "Ubuntu 22.04 LTS" }).key).toBe("linux");
    expect(osBadge({ osName: "Linux" }).key).toBe("linux");
  });
  it("everything else is other", () => {
    expect(osBadge({ osName: "FreeBSD" }).key).toBe("other");
    expect(osBadge({ osName: "" }).key).toBe("other");
  });
});

describe("sticky overrides", () => {
  it("do not reflow other hosts or later subnets", () => {
    const hosts = [
      { address: "10.0.1.1" }, { address: "10.0.1.2" }, { address: "10.0.2.1" },
    ];
    const plain = layoutHosts(hosts, new Map());
    const withSticky = layoutHosts(hosts, new Map([["10.0.1.1", { x: 500, y: 500 }]]));
    expect(withSticky.get("10.0.1.2")).toEqual(plain.get("10.0.1.2"));
    expect(withSticky.get("10.0.2.1")).toEqual(plain.get("10.0.2.1"));
  });
});
