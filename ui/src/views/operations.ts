import type { OperationState } from "../protocol/types";

// Pure helpers for the operations table, kept out of the component so they
// can be tested without opening a websocket.

export function operationsRows(list: readonly (OperationState | undefined)[]): OperationState[] {
  return list.filter((op): op is OperationState => !!op);
}

export function operationProgress(op: OperationState): string {
  if (op.total > 0) return `${op.done}/${op.total}`;
  return op.done > 0 ? String(op.done) : "-";
}

export function runningCount(list: readonly (OperationState | undefined)[]): number {
  return list.filter(op => op?.status === "running").length;
}
