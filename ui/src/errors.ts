import { CommandError } from "./ws/client";

// One formatting rule for command failures across dialogs and views. Keeping
// the RPC code visible makes "invalid_option" distinguishable from a timeout
// without each caller repeating the ternary.
export function errorMessage(e: unknown): string {
  if (e instanceof CommandError) return `${e.code}: ${e.message}`;
  if (e instanceof Error) return e.message;
  return String(e);
}
