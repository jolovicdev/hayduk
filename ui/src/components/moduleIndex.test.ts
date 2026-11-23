import { describe, expect, it } from "vitest";
import { buildModuleIndex, searchModuleIndex } from "./moduleIndex";

describe("moduleIndex", () => {
  const index = buildModuleIndex({
    exploits: [
      "windows/smb/ms17_010_eternalblue",
      "windows/smb/psexec",
      "linux/http/apache_mod_cgi_bash_env_exec",
    ],
    auxiliary: [
      "scanner/smb/smb_login",
      "scanner/ssh/ssh_version",
    ],
    post: ["windows/gather/hashdump"],
    payloads: [],
    encoders: [],
    evasion: [],
    nops: [],
  });

  it("matches multi-term queries across type and path", () => {
    const hits = searchModuleIndex(index, ["exploit", "smb"]);
    expect(hits.map(h => h.path)).toEqual([
      "windows/smb/ms17_010_eternalblue",
      "windows/smb/psexec",
    ]);
  });

  it("prioritizes leaf-name matches before path-only matches", () => {
    const hits = searchModuleIndex(index, ["smb"]);
    expect(hits[0]!.path).toBe("scanner/smb/smb_login");
    expect(hits).toHaveLength(3);
  });

  it("respects the result limit", () => {
    expect(searchModuleIndex(index, ["windows"], 2)).toHaveLength(2);
  });
});
