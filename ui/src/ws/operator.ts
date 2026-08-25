import { setOperator } from "./client";

const OPERATOR_KEY = "hayduk.operator";

// The operator name survives reloads: a stored name must ride on outgoing
// commands again without waiting for the operator to re-enter it.
export function restoreOperator(storage: Pick<Storage, "getItem"> = localStorage): string {
  const name = storage.getItem(OPERATOR_KEY) ?? "";
  if (name) setOperator(name);
  return name;
}

export function saveOperator(name: string, storage: Pick<Storage, "setItem"> = localStorage) {
  storage.setItem(OPERATOR_KEY, name);
  setOperator(name);
}

export function forgetOperator(storage: Pick<Storage, "getItem" | "removeItem"> = localStorage) {
  storage.removeItem(OPERATOR_KEY);
  setOperator("");
}
