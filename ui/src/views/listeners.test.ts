import { describe, expect, it } from "vitest";
import { listenerRows } from "./listeners";
import type { JobRow } from "./jobs";
import type { ListenerState } from "../protocol/types";

const job = (id: string, module: string): JobRow => ({ id, kind: "Exploit", module });

describe("listenerRows", () => {
  it("keeps only handler jobs and attaches their registry metadata", () => {
    const listeners: ListenerState[] = [{
      jobId: "3", payload: "linux/x64/meterpreter/reverse_tcp",
      lhost: "10.0.0.5", lport: 4444,
    }];
    const rows = listenerRows(
      [job("1", "scanner/ssh/ssh_login"), job("3", "Exploit: multi/handler")],
      listeners,
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]!.id).toBe("3");
    expect(rows[0]!.listener?.payload).toBe("linux/x64/meterpreter/reverse_tcp");
  });

  it("surfaces a persisted listener the job list missed", () => {
    const rows = listenerRows([], [{
      jobId: "9", payload: "windows/x64/meterpreter/reverse_tcp",
      lhost: "10.0.0.5", lport: 4444, startedAt: "2026-11-04T12:00:00Z",
    }]);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.module).toBe("multi/handler");
    expect(rows[0]!.startedAt).toBe("2026-11-04T12:00:00Z");
  });

  it("does not duplicate a job that also has a registry row", () => {
    const rows = listenerRows(
      [job("5", "Exploit: multi/handler")],
      [{ jobId: "5", payload: "p" }],
    );
    expect(rows).toHaveLength(1);
  });

  it("tolerates holes in the wire array", () => {
    const rows = listenerRows([], [undefined, { jobId: "7" }]);
    expect(rows).toHaveLength(1);
  });
});
