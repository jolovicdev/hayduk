import { describe, expect, it } from "vitest";
import { escapeCloser } from "./escape";

describe("escapeCloser", () => {
  it("closes the dialog on Escape", () => {
    const closed: number[] = [];
    const h = escapeCloser({ onClose: () => closed.push(1) });
    h({ key: "Escape" });
    expect(closed).toEqual([1]);
  });

  it("ignores keys other than Escape", () => {
    const closed: number[] = [];
    const h = escapeCloser({ onClose: () => closed.push(1) });
    h({ key: "Enter" });
    h({ key: "a" });
    expect(closed).toEqual([]);
  });

  it("lets an open overlay have the first Escape", () => {
    const closed: number[] = [];
    const h = escapeCloser({ onClose: () => closed.push(1), overlayOpen: () => true });
    h({ key: "Escape" });
    expect(closed).toEqual([]);
  });
});
