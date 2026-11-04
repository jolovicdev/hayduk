import { describe, expect, it } from "vitest";
import { operationProgress, operationsRows, runningCount } from "./operations";
import type { OperationState } from "../protocol/types";

function op(partial: Partial<OperationState>): OperationState {
  return {
    id: "op-1", kind: "services", status: "running", done: 0, total: 0,
    ...partial,
  };
}

describe("operationsRows", () => {
  it("drops holes from the wire array", () => {
    expect(operationsRows([op({ id: "a" }), undefined, op({ id: "b" })])).toHaveLength(2);
  });
});

describe("operationProgress", () => {
  it("shows a fraction for planned runs", () => {
    expect(operationProgress(op({ done: 3, total: 10 }))).toBe("3/10");
  });

  it("shows a bare count for unknown totals", () => {
    expect(operationProgress(op({ done: 4 }))).toBe("4");
    expect(operationProgress(op({ done: 0 }))).toBe("-");
  });
});

describe("runningCount", () => {
  it("counts only running operations", () => {
    expect(runningCount([
      op({ id: "a" }),
      op({ id: "b", status: "done" }),
      op({ id: "c", status: "canceled" }),
      undefined,
    ])).toBe(1);
  });
});
