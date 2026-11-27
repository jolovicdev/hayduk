import { describe, expect, it } from "vitest";
import { LOGIN_MODULES, loginOptions, looksLikeNtlmHash, rankCreds } from "./login";
import { serviceOpen } from "./serviceState";

describe("loginOptions", () => {
  it("offers test and session modules for smb and ssh", () => {
    const out = loginOptions([
      { port: 445, name: "smb" },
      { port: 22, name: "ssh" },
    ]);
    expect(out.map(m => m.module)).toEqual([
      "scanner/smb/smb_login", "windows/smb/psexec",
      "scanner/ssh/ssh_login", "multi/ssh/sshexec",
    ]);
  });

  it("matches on service name alone", () => {
    expect(loginOptions([{ port: 4444, name: "microsoft-ds" }]).map(m => m.module))
      .toEqual(["scanner/smb/smb_login", "windows/smb/psexec"]);
  });

  it("matches on port when the service is unnamed", () => {
    expect(loginOptions([{ port: 22, name: "" }]).map(m => m.module))
      .toEqual(["scanner/ssh/ssh_login", "multi/ssh/sshexec"]);
  });

  it("offers winrm on 5985", () => {
    expect(loginOptions([{ port: 5985, name: "winrm" }]).map(m => m.module))
      .toEqual(["scanner/winrm/winrm_login", "windows/winrm/winrm_script_exec"]);
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
    const smb = LOGIN_MODULES.find(m => m.module === "scanner/smb/smb_login")!;
    expect(smb.userKey).toBe("SMBUser");
    expect(smb.domainKey).toBe("SMBDomain");
    const ssh = LOGIN_MODULES.find(m => m.module === "scanner/ssh/ssh_login")!;
    expect(ssh.userKey).toBe("USERNAME");
  });

  it("session modules are exploits with curated payloads", () => {
    const psexec = LOGIN_MODULES.find(m => m.module === "windows/smb/psexec")!;
    expect(psexec.kind).toBe("session");
    expect(psexec.moduleType).toBe("exploit");
    expect(psexec.payloads).toContain("windows/x64/meterpreter/reverse_tcp");
    for (const m of LOGIN_MODULES.filter(m => m.kind === "session")) {
      expect(m.payloads?.length).toBeGreaterThan(0);
    }
    for (const m of LOGIN_MODULES.filter(m => m.kind === "test")) {
      expect(m.moduleType).toBe("auxiliary");
    }
  });
});

describe("looksLikeNtlmHash", () => {
  it("accepts bare NTLM and LM:NTLM pairs", () => {
    expect(looksLikeNtlmHash("aad3b435b51404eeaad3b435b51404ee")).toBe(true);
    expect(looksLikeNtlmHash("AAD3B435B51404EEAAD3B435B51404EE:31d6cfe0d16ae931b73c59d7e0c089c0")).toBe(true);
  });
  it("rejects passwords and malformed hashes", () => {
    expect(looksLikeNtlmHash("P@ssw0rd!")).toBe(false);
    expect(looksLikeNtlmHash("aad3b435b51404ee")).toBe(false);
    expect(looksLikeNtlmHash("")).toBe(false);
    expect(looksLikeNtlmHash("zzzzb435b51404eeaad3b435b51404ee")).toBe(false);
  });
});

describe("closed services", () => {
  it("excludes closed ports from login options", () => {
    expect(loginOptions([{ port: 445, name: "smb", state: "closed" }])).toEqual([]);
    expect(loginOptions([{ port: 22, name: "ssh", state: "filtered" }])).toEqual([]);
  });
  it("keeps services whose state is empty or open", () => {
    const want = ["scanner/smb/smb_login", "windows/smb/psexec"];
    expect(loginOptions([{ port: 445, name: "smb", state: "" }]).map(m => m.module)).toEqual(want);
    expect(loginOptions([{ port: 445, name: "smb", state: "open" }]).map(m => m.module)).toEqual(want);
    expect(loginOptions([{ port: 445, name: "smb" }]).map(m => m.module)).toEqual(want);
  });
  it("falls back to an open service when another is closed", () => {
    const out = loginOptions([
      { port: 139, name: "netbios-ssn", state: "closed" },
      { port: 1445, name: "smb", state: "open" },
    ]);
    expect(out.map(m => m.module)).toEqual(["scanner/smb/smb_login", "windows/smb/psexec"]);
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
