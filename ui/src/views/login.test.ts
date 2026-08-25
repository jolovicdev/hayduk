import { describe, expect, it } from "vitest";
import { LOGIN_MODULES, loginOptions, rankCreds } from "./login";

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
