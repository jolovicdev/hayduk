import { describe, expect, it } from "vitest";
import { HISTORY_CAP, applyCompletion, pushHistory, recallHistory } from "./consoleInput";

describe("pushHistory", () => {
  it("appends commands, skips empties and consecutive repeats", () => {
    let h: string[] = [];
    h = pushHistory(h, "version");
    h = pushHistory(h, "");
    h = pushHistory(h, "version");
    h = pushHistory(h, "workspace");
    expect(h).toEqual(["version", "workspace"]);
  });

  it("is capped", () => {
    let h: string[] = [];
    for (let i = 0; i < HISTORY_CAP + 50; i++) h = pushHistory(h, `cmd-${i}`);
    expect(h.length).toBe(HISTORY_CAP);
    expect(h[0]).toBe(`cmd-50`);
    expect(h.at(-1)).toBe(`cmd-${HISTORY_CAP + 49}`);
  });
});

describe("recallHistory", () => {
  const h = ["a", "b", "c"]; // c is the newest
  it("arrow up walks back through time, down forward, -1 is the draft", () => {
    expect(recallHistory(h, -1, "up")).toEqual({ idx: 0, text: "c" });
    expect(recallHistory(h, 0, "up")).toEqual({ idx: 1, text: "b" });
    expect(recallHistory(h, 1, "up")).toEqual({ idx: 2, text: "a" });
    expect(recallHistory(h, 2, "down")).toEqual({ idx: 1, text: "b" });
    expect(recallHistory(h, 0, "down")).toEqual({ idx: -1, text: "" });
  });

  it("clamps at both ends", () => {
    expect(recallHistory(h, 2, "up")).toEqual({ idx: 2, text: "a" });
    expect(recallHistory(h, -1, "down")).toEqual({ idx: -1, text: "" });
  });
});

describe("applyCompletion", () => {
  // msf's console.tabs returns full replacements for the last token
  it("single option replaces the token", () => {
    expect(applyCompletion("use windows/smb/ms", ["windows/smb/ms17_010_eternalblue"], "ms"))
      .toBe("use windows/smb/ms17_010_eternalblue");
  });

  it("multiple options extend to the longest common token prefix", () => {
    expect(applyCompletion("use scanner/ssh/ssh_lo",
      ["scanner/ssh/ssh_login", "scanner/ssh/ssh_login_pubkey"], "ssh_lo"))
      .toBe("use scanner/ssh/ssh_login");
  });

  it("returns null when nothing extends the token", () => {
    expect(applyCompletion("vulns", ["vulns"], "vulns")).toBeNull();
    expect(applyCompletion("zz", [], "zz")).toBeNull();
  });
});
