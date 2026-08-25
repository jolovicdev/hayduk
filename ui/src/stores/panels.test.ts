import { describe, expect, it } from "vitest";
import { DEFAULT_PANELS, clampNotebook, clampPanels } from "./panels";

describe("panel sizes", () => {
  it("clamps every panel to its range", () => {
    const p = clampPanels({ left: 9999, right: -5, nb: 9999 }, 1080);
    expect(p).toEqual({ left: 560, right: 250, nb: 780 });
  });

  it("keeps valid sizes untouched", () => {
    expect(clampPanels({ left: 300, right: 320, nb: 214 }, 900))
      .toEqual({ left: 300, right: 320, nb: 214 });
  });

  it("falls back to defaults on corrupt input", () => {
    expect(clampPanels({ left: NaN, right: 320, nb: 214 }, 900))
      .toEqual({ ...DEFAULT_PANELS, right: 320, nb: 214 });
  });

  it("never lets the notebook maximum fall below its minimum", () => {
    // short viewports used to produce max < min, collapsing the notebook
    expect(clampNotebook(214, 350)).toBe(140);
    expect(clampNotebook(214, 480)).toBe(180);
    expect(clampNotebook(999, 1080)).toBe(780);
  });
});
