import { describe, expect, it } from "vitest";
import { GROUP_HEADER, GROUP_INSET, NH, NW, cidrCovers, fitLabel, geometrySignature, layoutHosts, layoutPlan, pivotPath, subnetGroups, subnetKey } from "./layout";
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

describe("cidrCovers", () => {
  it("honors the prefix width across groups", () => {
    expect(cidrCovers("10.0.0.0/8", "10.200.3.4")).toBe(true);
    expect(cidrCovers("10.0.0.0/16", "10.0.99.4")).toBe(true);
    expect(cidrCovers("10.0.0.0/24", "10.0.1.4")).toBe(false);
    expect(cidrCovers("192.168.0.0/22", "192.168.3.255")).toBe(true);
    expect(cidrCovers("192.168.0.0/22", "192.168.4.0")).toBe(false);
  });
  it("handles the extreme prefixes", () => {
    expect(cidrCovers("0.0.0.0/0", "203.0.113.7")).toBe(true);
    expect(cidrCovers("10.0.0.5/32", "10.0.0.5")).toBe(true);
    expect(cidrCovers("10.0.0.5/32", "10.0.0.6")).toBe(false);
  });
  it("treats a prefixless route as one host", () => {
    expect(cidrCovers("10.0.0.5", "10.0.0.5")).toBe(true);
    expect(cidrCovers("10.0.0.5", "10.0.0.6")).toBe(false);
  });
  it("covers nothing when the route is not strict IPv4 CIDR", () => {
    expect(cidrCovers("dead:beef::/64", "dead:beef::1")).toBe(false);
    expect(cidrCovers("dead:beef::/64", "10.0.0.1")).toBe(false);
    expect(cidrCovers("10.0.0.0/", "10.0.0.1")).toBe(false); // empty prefix is not 0
    expect(cidrCovers("10.0.0.0/33", "10.0.0.1")).toBe(false);
    expect(cidrCovers("10.0.0.0/-1", "10.0.0.1")).toBe(false);
    expect(cidrCovers("300.0.0.0/8", "10.0.0.1")).toBe(false);
  });
  it("never covers a non-IPv4 host", () => {
    expect(cidrCovers("10.0.0.0/8", "hostname")).toBe(false);
    expect(cidrCovers("10.0.0.0/8", "")).toBe(false);
    expect(cidrCovers("10.0.0.0/8", "::1")).toBe(false);
  });
});

describe("geometrySignature", () => {
  const base = {
    hosts: [{ address: "10.0.0.1" }, { address: "10.0.0.2" }],
    routes: [{ subnet: "10.9.0.0/24", sessionId: "3" }],
    sessions: { "3": { targetHost: "10.0.0.1" } },
  };
  const sig = (hosts: any, routes: any, sessions: any) =>
    geometrySignature(hosts, routes, sessions);

  it("is stable when equal geometry arrives as fresh objects", () => {
    // every resource update replaces the whole campaign state object; the
    // signature must key on values, never on identity, or unrelated churn
    // (jobs, creds, operators, ranks arriving in the same new state) would
    // refit the view
    const fresh = JSON.parse(JSON.stringify(base));
    expect(sig(fresh.hosts, fresh.routes, fresh.sessions))
      .toBe(sig(base.hosts, base.routes, base.sessions));
  });
  it("moves when a host appears or leaves", () => {
    expect(sig([...base.hosts, { address: "10.0.1.1" }], base.routes, base.sessions))
      .not.toBe(sig(base.hosts, base.routes, base.sessions));
    expect(sig([base.hosts[0]], base.routes, base.sessions))
      .not.toBe(sig(base.hosts, base.routes, base.sessions));
  });
  it("moves when a route or its source session's host moves", () => {
    expect(sig(base.hosts, [{ subnet: "10.9.0.0/16", sessionId: "3" }], base.sessions))
      .not.toBe(sig(base.hosts, base.routes, base.sessions));
    expect(sig(base.hosts, base.routes, { "3": { targetHost: "10.0.0.2" } }))
      .not.toBe(sig(base.hosts, base.routes, base.sessions));
    // sessionHost is the fallback source; same host, same geometry
    expect(sig(base.hosts, base.routes, { "3": { sessionHost: "10.0.0.1" } }))
      .toBe(sig(base.hosts, base.routes, base.sessions));
    expect(sig(base.hosts, base.routes, { "3": { sessionHost: "10.0.0.2" } }))
      .not.toBe(sig(base.hosts, base.routes, base.sessions));
  });
  it("is stable for session churn that keeps the source host", () => {
    expect(sig(base.hosts, base.routes, {
      "3": { targetHost: "10.0.0.1", username: "root", viaExploit: "x" },
    })).toBe(sig(base.hosts, base.routes, base.sessions));
  });
  it("tolerates null host and route entries", () => {
    expect(sig([...base.hosts, null], [...base.routes, null], base.sessions))
      .toBe(sig(base.hosts, base.routes, base.sessions));
  });
});

describe("adaptive layout", () => {
  const hosts = Array.from({ length: 6 }, (_, i) => ({ address: `10.0.1.${i + 1}` }));

  it("uses taller rows when a pivot destination consumes canvas width", () => {
    expect(layoutPlan(hosts, 920, 570, 320)).toEqual({ columns: 2, groupColumns: 1 });
    expect(layoutPlan(hosts, 1200, 400, 0)).toEqual({ columns: 3, groupColumns: 1 });
  });

  it("spreads a many-subnet campaign across stacks instead of one tall rail", () => {
    // 530 hosts over 8 /24s on a wide, short stage: the fitted overview
    // must stay legible, which one vertical stack of groups cannot deliver
    const campaign = Array.from({ length: 8 }, (_, subnet) =>
      Array.from({ length: 66 }, (_, i) => ({ address: `10.0.${subnet + 1}.${i + 1}` }))).flat();
    const plan = layoutPlan(campaign, 1392, 204, 0);
    expect(plan.groupColumns).toBeGreaterThan(1);
    expect(plan.columns).toBeGreaterThan(3);

    const positions = layoutHosts(campaign, new Map(), plan.columns, plan.groupColumns);
    const groups = [...subnetGroups(campaign, positions).values()];
    const world = {
      w: Math.max(...groups.map(g => g.x + g.w)) - Math.min(...groups.map(g => g.x)),
      h: Math.max(...groups.map(g => g.y + g.h)) - Math.min(...groups.map(g => g.y)),
    };
    // fit must actually fit, and stay legible enough to read a zone label
    expect(Math.min(1392 / world.w, 204 / world.h)).toBeGreaterThan(0.08);
  });

  it("keeps subnet stacks side by side without overlap", () => {
    const twoSubnets = [
      { address: "10.0.1.1" }, { address: "10.0.1.2" },
      { address: "10.0.2.1" }, { address: "10.0.2.2" },
    ];
    const positions = layoutHosts(twoSubnets, new Map(), 2, 2);
    const groups = [...subnetGroups(twoSubnets, positions).values()].sort((a, b) => a.x - b.x);
    expect(groups).toHaveLength(2);
    expect(groups[1]!.x).toBeGreaterThanOrEqual(groups[0]!.x + groups[0]!.w);
  });

  it("gaps every pair of groups in a stack, the first pair included", () => {
    const three = [{ address: "10.0.1.1" }, { address: "10.0.2.1" }, { address: "10.0.3.1" }];
    const positions = layoutHosts(three, new Map(), 1, 1);
    const groups = [...subnetGroups(three, positions).values()].sort((a, b) => a.y - b.y);
    for (let i = 1; i < groups.length; i++) {
      expect(groups[i]!.y - (groups[i - 1]!.y + groups[i - 1]!.h)).toBe(40);
    }
  });

  it("keeps every automatic card inside its subnet without overlap", () => {
    const multiple = [...hosts, { address: "10.0.2.1" }, { address: "10.0.2.2" }];
    for (const columns of [1, 2, 3, 4]) {
      const positions = layoutHosts(multiple, new Map(), columns);
      const groups = subnetGroups(multiple, positions);
      for (const host of multiple) {
        const p = positions.get(host.address)!;
        const group = groups.get(subnetKey(host.address))!;
        expect(p.x).toBeGreaterThanOrEqual(group.x + GROUP_INSET);
        expect(p.y).toBeGreaterThanOrEqual(group.y + GROUP_HEADER);
        expect(p.x + NW).toBeLessThanOrEqual(group.x + group.w);
        expect(p.y + NH).toBeLessThanOrEqual(group.y + group.h);
        for (const other of multiple.filter(h => h.address !== host.address)) {
          const q = positions.get(other.address)!;
          expect(p.x + NW <= q.x || q.x + NW <= p.x || p.y + NH <= q.y || q.y + NH <= p.y).toBe(true);
        }
      }
    }
  });

  it("keeps a dragged position when the canvas changes columns", () => {
    const sticky = new Map([[hosts[0]!.address, { x: 900, y: 300 }]]);
    expect(layoutHosts(hosts, sticky, 2).get(hosts[0]!.address)).toEqual({ x: 900, y: 300 });
  });

  it("exits below a host before crossing its row toward the pivot lane", () => {
    const positions = layoutHosts(hosts, new Map(), 3);
    const source = positions.get(hosts[0]!.address)!;
    const group = subnetGroups(hosts, positions).get("10.0.1")!;
    const from = { x: source.x + NW / 2, y: source.y + NH };
    const lane = group.x + group.w + 48;
    const path = pivotPath(from, { x: lane + 60, y: source.y + NH / 2 }, lane);
    const rowLane = Number(path.match(/V ([\d.]+)/)![1]);
    expect(rowLane).toBeGreaterThan(source.y + NH);
    expect(rowLane).toBeLessThan(positions.get(hosts[3]!.address)!.y);
    expect(lane).toBeGreaterThan(group.x + group.w);
  });
});
