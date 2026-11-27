import { describe, expect, it } from "vitest";
import { formatRhosts, hostsInMarquee, pruneHostSelection, toggleHostSelection } from "./selection";

describe("host selection helpers", () => {
  it("replaces on normal click and toggles on additive click", () => {
    const s1 = toggleHostSelection(new Set(["10.0.0.1"]), "10.0.0.2", false);
    expect([...s1]).toEqual(["10.0.0.2"]);

    const s2 = toggleHostSelection(s1, "10.0.0.3", true);
    expect([...s2]).toEqual(["10.0.0.2", "10.0.0.3"]);

    const s3 = toggleHostSelection(s2, "10.0.0.2", true);
    expect([...s3]).toEqual(["10.0.0.3"]);
  });

  it("intersects marquee boxes regardless of drag direction", () => {
    const positions = new Map([
      ["10.0.0.1", { x: 0, y: 0 }],
      ["10.0.0.2", { x: 250, y: 0 }],
      ["10.0.0.3", { x: 600, y: 400 }],
    ]);
    const hits = hostsInMarquee(positions, { x1: 300, y1: 100, x2: 10, y2: 10 }, 200, 120);
    expect(hits).toEqual(["10.0.0.1", "10.0.0.2"]);
  });

  it("prunes stale addresses and formats deduplicated RHOSTS", () => {
    expect([...pruneHostSelection(new Set(["10.0.0.1", "10.0.0.9"]), new Set(["10.0.0.1"]))])
      .toEqual(["10.0.0.1"]);
    expect(formatRhosts(["10.0.0.1", " 10.0.0.2 ", "10.0.0.1", ""])).toBe("10.0.0.1 10.0.0.2");
  });
});
