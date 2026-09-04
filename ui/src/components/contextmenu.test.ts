import { describe, expect, it, vi } from "vitest";
import { buildMenuHTML, openContextMenuFor, placeMenu, type MenuItem } from "./contextmenu";

describe("openContextMenuFor", () => {
  it("silences the event before the menu opens", () => {
    // the close listener shares Solid's delegated document node; without
    // the immediate stop it would close the freshly opened menu itself.
    // Opening itself is unobservable here: the menu element only exists
    // once ContextMenuRoot mounts, which this node-env suite lacks.
    const preventDefault = vi.fn();
    const stopImmediatePropagation = vi.fn();
    const e = {
      clientX: 10,
      clientY: 20,
      preventDefault,
      stopImmediatePropagation,
    } as unknown as MouseEvent;
    openContextMenuFor(e, [{ label: "Copy", fn: () => {} }]);
    expect(preventDefault).toHaveBeenCalledOnce();
    expect(stopImmediatePropagation).toHaveBeenCalledOnce();
    // silence lands in the written order, before the module opens anything
    expect(preventDefault.mock.invocationCallOrder[0]!).toBeLessThan(
      stopImmediatePropagation.mock.invocationCallOrder[0]!,
    );
  });
});

describe("menu items", () => {
  it("separates and labels items", () => {
    const items: MenuItem[] = [
      { head: "jmp-01", sub: "10.0.0.5" },
      { icon: "key", label: "Login as…" },
      { sep: true },
      { icon: "copy", label: "Copy address", hint: "10.0.0.5" },
      { icon: "x", label: "Kill session", danger: true },
    ];
    const html = buildMenuHTML(items);
    expect(html).toContain('class="chead"');
    expect(html).toContain("Login as…");
    expect(html).toContain('class="csep"');
    expect(html).toContain("Kill session");
    expect(html).toContain("danger");
    expect(html).toContain("Copy address");
  });

  it("escapes hostile text", () => {
    const html = buildMenuHTML([{ head: '<script>alert(1)</script>' }]);
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
  });
});

describe("placeMenu", () => {
  it("keeps the menu inside the viewport", () => {
    // a menu opened near the right edge shifts left instead of overflowing
    expect(placeMenu(1900, 400, 240, 300, 1920, 1080)).toEqual({ x: 1672, y: 400 });
    expect(placeMenu(1900, 900, 240, 300, 1920, 1080)).toEqual({ x: 1672, y: 772 });
  });

  it("never produces negative coordinates", () => {
    // a menu wider than the viewport stays pinned at the left edge
    expect(placeMenu(10, 10, 3000, 300, 1920, 1080)).toEqual({ x: 0, y: 10 });
    expect(placeMenu(5, 5, 240, 2000, 1920, 1080)).toEqual({ x: 5, y: 0 });
  });

  it("leaves roomy positions alone", () => {
    expect(placeMenu(100, 100, 240, 300, 1920, 1080)).toEqual({ x: 100, y: 100 });
  });
});
