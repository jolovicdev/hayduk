import { describe, expect, it } from "vitest";
import { jobRows, parseJobName } from "./jobs";

describe("parseJobName", () => {
  it("splits msf's kind prefix from the module path", () => {
    expect(parseJobName("Exploit: linux/http/apache_druid_js_rce"))
      .toEqual({ kind: "Exploit", module: "linux/http/apache_druid_js_rce" });
    expect(parseJobName("Auxiliary: scanner/ssh/ssh_login"))
      .toEqual({ kind: "Auxiliary", module: "scanner/ssh/ssh_login" });
  });

  it("passes through names without a prefix", () => {
    expect(parseJobName("meterpreter")).toEqual({ kind: "", module: "meterpreter" });
  });
});

describe("jobRows", () => {
  it("orders jobs numerically by id", () => {
    const rows = jobRows({
      "10": { id: "10", name: "Exploit: a/b", startedAt: "2026-08-25T10:00:00Z" },
      "2": { id: "2", name: "Exploit: c/d", startedAt: "2026-08-25T09:00:00Z" },
    });
    expect(rows.map(r => r.id)).toEqual(["2", "10"]);
    expect(rows[0]!.module).toBe("c/d");
  });

  it("tolerates garbage ids at the end", () => {
    const rows = jobRows({
      "udp-3": { id: "udp-3", name: "Auxiliary: x/y" },
      "1": { id: "1", name: "Exploit: a/b" },
    });
    expect(rows[0]!.id).toBe("1");
    expect(rows[1]!.id).toBe("udp-3");
  });
});
