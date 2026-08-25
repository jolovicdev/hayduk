import type { JobState } from "../protocol/types";

export interface JobRow {
  id: string;
  kind: string;
  module: string;
  startedAt?: string;
}

// msf job names arrive as "Exploit: linux/http/x" - split the kind from the
// module path so the table can show both cleanly.
export function parseJobName(name: string): { kind: string; module: string } {
  const idx = name.indexOf(": ");
  if (idx === -1) return { kind: "", module: name };
  return { kind: name.slice(0, idx), module: name.slice(idx + 2) };
}

// jobRows turns the jobs map into view rows ordered numerically by id;
// ids that are not plain numbers sort last, alphabetically.
export function jobRows(jobs: Record<string, JobState | undefined>): JobRow[] {
  return Object.values(jobs)
    .filter((j): j is JobState => !!j)
    .map(j => ({ id: j.id, startedAt: j.startedAt, ...parseJobName(j.name) }))
    .sort((a, b) => {
      const an = /^\d+$/.test(a.id) ? Number(a.id) : Infinity;
      const bn = /^\d+$/.test(b.id) ? Number(b.id) : Infinity;
      if (an !== bn) return an - bn;
      return a.id.localeCompare(b.id);
    });
}
