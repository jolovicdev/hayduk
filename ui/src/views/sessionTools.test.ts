import { describe, expect, it } from "vitest";
import { parseFiles, parsePortForwards, parseProcesses } from "./sessionTools";

describe("parseProcesses", () => {
  it("reads meterpreter ps rows", () => {
    const out = [
      "Process List",
      "============",
      "",
      " PID   PPID  Name               Arch  Session  User              Path",
      " ---   ----  ----               ----  -------  ----              ----",
      " 4     0     System             x64   0",
      " 812   4     smss.exe           x64   0        NT AUTHORITY\\SYSTEM",
      " 2312  812   explorer.exe       x64   1        WS1\\alice",
    ].join("\n");
    const rows = parseProcesses(out);
    expect(rows.map(r => r.pid)).toEqual(["4", "812", "2312"]);
    expect(rows[2]).toMatchObject({ name: "explorer.exe", ppid: "812" });
  });
  it("ignores prose", () => {
    expect(parseProcesses("no processes here")).toEqual([]);
  });
});

describe("parseFiles", () => {
  it("reads meterpreter ls rows", () => {
    const out = [
      "Listing: C:\\Users\\alice",
      "=======================",
      "",
      "Mode              Size   Type  Last modified              Name",
      "----              ----   ----  -------------              ----",
      "100666/rw-rw-rw-  120    fil   2026-01-01 10:00:00 +0100  notes.txt",
      "40777/rwxrwxrwx   0      dir   2026-01-01 10:00:00 +0100  Documents",
    ].join("\n");
    const rows = parseFiles(out);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ name: "notes.txt", kind: "fil" });
    expect(rows[1]).toMatchObject({ name: "Documents", kind: "dir" });
  });
});

describe("parsePortForwards", () => {
  it("reads indexed forward rows", () => {
    const out = "Active Port Forwards\n====================\n\n   Index  Forward          Destination\n   -----  -------          -----------\n   1      *:3389           10.0.0.9:3389\n";
    expect(parsePortForwards(out)).toEqual([{ local: "*:3389", remote: "10.0.0.9:3389" }]);
  });
});
