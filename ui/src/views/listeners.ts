import type { JobRow } from "./jobs";
import type { ListenerState } from "../protocol/types";

// Merge the daemon's live handler jobs with the engine's persisted launch
// registry. Jobs decide what is running; the registry supplies payload and
// callback metadata, and can also surface a row whose job seed was missed.
// Web delivery jobs are listeners too: they serve a payload and catch the
// same callbacks.
const LISTENER_MODULES = ["multi/handler", "multi/script/web_delivery"];

export function listenerRows(
  jobs: JobRow[],
  listeners: readonly (ListenerState | undefined)[],
): (JobRow & { listener?: ListenerState })[] {
  const byJob = new Map<string, ListenerState>();
  for (const l of listeners) {
    if (l) byJob.set(l.jobId, l);
  }
  const rows: (JobRow & { listener?: ListenerState })[] = [];
  const seen = new Set<string>();
  for (const job of jobs) {
    if (!LISTENER_MODULES.some(m => job.module.includes(m))) continue;
    seen.add(job.id);
    rows.push({ ...job, listener: byJob.get(job.id) });
  }
  for (const [id, listener] of byJob) {
    if (seen.has(id)) continue;
    rows.push({
      id, kind: "Exploit", module: "multi/handler",
      startedAt: listener.startedAt, listener,
    });
  }
  return rows;
}
