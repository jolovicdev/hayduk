import { describe, expect, it } from "vitest";
import { joinRemote, parentRemote, parsePortForwards, parseProcesses } from "./sessionTools";

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

describe("remote path navigation", () => {
  it("joins with the remote separator", () => {
    expect(joinRemote("/home/msfadmin", "docs", "/")).toBe("/home/msfadmin/docs");
    expect(joinRemote("C:\\Users", "alice", "\\")).toBe("C:\\Users\\alice");
    expect(joinRemote("C:\\", "Windows", "\\")).toBe("C:\\Windows");
    expect(joinRemote("/", "etc", "/")).toBe("/etc");
  });
  it("walks up to the root and stops there", () => {
    expect(parentRemote("/home/msfadmin", "/")).toBe("/home");
    expect(parentRemote("/home", "/")).toBe("/");
    expect(parentRemote("/", "/")).toBe("/");
    expect(parentRemote("C:\\Users\\alice", "\\")).toBe("C:\\Users");
    expect(parentRemote("C:\\Users", "\\")).toBe("C:\\");
    expect(parentRemote("C:\\", "\\")).toBe("C:\\");
  });
});

describe("parsePortForwards", () => {
  it("reads indexed forward rows", () => {
    const out = "Active Port Forwards\n====================\n\n   Index  Forward          Destination\n   -----  -------          -----------\n   1      *:3389           10.0.0.9:3389\n";
    expect(parsePortForwards(out)).toEqual([{ local: "*:3389", remote: "10.0.0.9:3389" }]);
  });
});
