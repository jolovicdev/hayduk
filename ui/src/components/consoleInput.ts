// Console input state: bounded history and shell-style completion. Pure so
// the fiddly edges stay tested.

export const HISTORY_CAP = 500;

export function pushHistory(history: string[], cmd: string): string[] {
  const trimmed = cmd.trim();
  if (!trimmed || history.at(-1) === trimmed) return history;
  const next = [...history, trimmed];
  return next.length > HISTORY_CAP ? next.slice(next.length - HISTORY_CAP) : next;
}

// recallHistory maps arrow keys onto history positions; idx -1 is the fresh
// draft, 0 the oldest entry.
export function recallHistory(
  history: string[],
  idx: number,
  key: "up" | "down",
): { idx: number; text: string } {
  const next = key === "up" ? Math.min(idx + 1, history.length - 1) : Math.max(idx - 1, -1);
  return { idx: next, text: next === -1 ? "" : (history[history.length - 1 - next] ?? "") };
}

// applyCompletion rewrites the last word of `line` from the completion
// options; null when the options add nothing beyond what was typed.
export function applyCompletion(line: string, options: string[], fragment: string): string | null {
  if (options.length === 0) return null;
  const parts = line.split(/\s+/);
  if (options.length === 1) {
    if (options[0] === parts[parts.length - 1]) return null;
    parts[parts.length - 1] = options[0]!;
    return parts.join(" ");
  }
  let prefix = options[0]!;
  for (const o of options.slice(1)) {
    while (!o.startsWith(prefix)) prefix = prefix.slice(0, -1);
  }
  if (prefix.length <= fragment.length) return null;
  parts[parts.length - 1] = prefix;
  return parts.join(" ");
}
