import { describe, expect, it } from "vitest";
import { LOGIN_MODULES, loginOptions, rankCreds } from "./login";
import { serviceOpen } from "./serviceState";

describe("loginOptions", () => {
  it("offers smb and ssh for a host running both", () => {
    const out = loginOptions([
      { port: 445, name: "smb" },
      { port: 22, name: "ssh" },
    ]);
    expect(out.map(m => m.module)).toEqual(["scanner/smb/smb_login", "scanner/ssh/ssh_login"]);
  });

  it("matches on service name alone", () => {
    expect(loginOptions([{ port: 4444, name: "microsoft-ds" }]).map(m => m.label)).toEqual(["SMB"]);
  });

  it("matches on port when the service is unnamed", () => {
    expect(loginOptions([{ port: 22, name: "" }]).map(m => m.label)).toEqual(["SSH"]);
  });

  it("carries the matched port along, including SMB on 139", () => {
    const smb = loginOptions([{ port: 139, name: "netbios-ssn" }]);
    expect(smb[0]!.port).toBe(139);
    const ssh = loginOptions([{ port: 2222, name: "ssh" }]);
    expect(ssh[0]!.port).toBe(2222);
  });

  it("offers nothing for unrelated services", () => {
    expect(loginOptions([{ port: 80, name: "http" }])).toEqual([]);
  });
});

describe("rankCreds", () => {
  it("puts complete user/password pairs first", () => {
    const out = rankCreds([
      { user: "root" },
      { user: "admin", pass: "s3cret" },
      { user: "guest", pass: "" },
    ]);
    expect(out[0]).toEqual({ user: "admin", pass: "s3cret" });
  });

  it("keeps every credential", () => {
    const creds = [{ user: "a" }, { user: "b" }];
    expect(rankCreds(creds)).toHaveLength(2);
  });
});

describe("LOGIN_MODULES option keys", () => {
  it("uses the module's own option names", () => {
    const smb = LOGIN_MODULES.find(m => m.label === "SMB")!;
    expect(smb.userKey).toBe("SMBUser");
    const ssh = LOGIN_MODULES.find(m => m.label === "SSH")!;
    expect(ssh.userKey).toBe("USERNAME");
  });
});

describe("closed services", () => {
  it("excludes closed ports from login options", () => {
    expect(loginOptions([{ port: 445, name: "smb", state: "closed" }])).toEqual([]);
    expect(loginOptions([{ port: 22, name: "ssh", state: "filtered" }])).toEqual([]);
  });
  it("keeps services whose state is empty or open", () => {
    expect(loginOptions([{ port: 445, name: "smb", state: "" }]).map(m => m.label)).toEqual(["SMB"]);
    expect(loginOptions([{ port: 445, name: "smb", state: "open" }]).map(m => m.label)).toEqual(["SMB"]);
    expect(loginOptions([{ port: 445, name: "smb" }]).map(m => m.label)).toEqual(["SMB"]);
  });
  it("falls back to an open service when another is closed", () => {
    const out = loginOptions([
      { port: 139, name: "netbios-ssn", state: "closed" },
      { port: 1445, name: "smb", state: "open" },
    ]);
    expect(out.map(m => m.label)).toEqual(["SMB"]);
    expect(out[0]!.port).toBe(1445);
  });
});

describe("serviceOpen", () => {
  it("counts empty and open states, nothing else", () => {
    expect(serviceOpen({ state: "" })).toBe(true);
    expect(serviceOpen({})).toBe(true);
    expect(serviceOpen({ state: "open" })).toBe(true);
    expect(serviceOpen({ state: "closed" })).toBe(false);
    expect(serviceOpen({ state: "filtered" })).toBe(false);
  });
});
