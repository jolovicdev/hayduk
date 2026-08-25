import { beforeEach, describe, expect, it } from "vitest";
import { currentOperator, setOperator } from "./client";
import { forgetOperator, restoreOperator, saveOperator } from "./operator";

describe("operator identity", () => {
  beforeEach(() => setOperator(""));

  it("restores a stored name into outgoing commands", () => {
    const storage = { getItem: () => "dana" };
    expect(restoreOperator(storage)).toBe("dana");
    expect(currentOperator()).toBe("dana");
  });

  it("is a no-op when nothing is stored", () => {
    expect(restoreOperator({ getItem: () => null })).toBe("");
    expect(currentOperator()).toBe("");
  });

  it("saving stamps the live operator too", () => {
    const stored: Record<string, string> = {};
    saveOperator("mile", { setItem: (k, v) => { stored[k] = v; } });
    expect(stored["hayduk.operator"]).toBe("mile");
    expect(currentOperator()).toBe("mile");
  });

  it("forgetting clears both", () => {
    const saved: Record<string, string> = { "hayduk.operator": "dana" };
    saveOperator("dana", { setItem: (k, v) => { saved[k] = v; } });
    forgetOperator({
      getItem: (k) => saved[k] ?? null,
      removeItem: (k) => { delete saved[k]; },
    });
    expect(currentOperator()).toBe("");
  });
});
