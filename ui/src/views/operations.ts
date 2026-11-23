import type { OperationState } from "../protocol/types";

// Pure helpers for the operations table, kept out of the component so they
// can be tested without opening a websocket.

export function operationsRows(list: readonly (OperationState | undefined)[]): OperationState[] {
  return list.filter((op): op is OperationState => !!op);
}

export function operationProgress(op: OperationState): string {
  const done = Number.isFinite(op.done) ? op.done : 0;
  if (op.total > 0) return `${done}/${op.total}`;
  return done > 0 ? String(done) : "-";
}

export function runningCount(list: readonly (OperationState | undefined)[]): number {
  return list.filter(op => op?.status === "running").length;
}
