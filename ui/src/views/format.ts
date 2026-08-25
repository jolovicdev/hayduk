// Small display formatters kept pure and tested.

export function ageOf(openedAt: string | undefined, now = Date.now()): string {
  if (!openedAt) return "-";
  const opened = new Date(openedAt).getTime();
  if (!Number.isFinite(opened)) return "-";
  const s = Math.max(0, (now - opened) / 1000);
  if (s < 60) return `${Math.floor(s)}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s / 3600)}h`;
}

export function interactPrompt(sessionType: string | undefined, sid: string): string {
  if (sessionType === "meterpreter") return `meterpreter ${sid} > `;
  return `${sid} sh > `;
}
