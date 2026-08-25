import { describe, expect, it } from "vitest";
import { buildModuleTree, filterTree, visibleChildren } from "./tree";

// the wire ModuleIndex carries plural collection keys; msf module types are
// singular and module refnames are relative to their type
describe("buildModuleTree", () => {
  it("nests module paths and counts leaves", () => {
    const root = buildModuleTree({
      exploits: ["windows/smb/ms08_067_netapi", "windows/http/rejetto_hfs", "multi/http/tomcat_mgr_upload"],
      auxiliary: ["scanner/ssh/ssh_login"],
    });
    expect(root.children.map(c => c.name)).toEqual(["auxiliary", "exploits"]);
    const exploits = root.children[1]!;
    expect(exploits.count).toBe(3);
    const windows = exploits.children.find(c => c.name === "windows")!;
    expect(windows.children.map(c => c.name)).toEqual(["http", "smb"]);
    const smb = windows.children[1]!;
    expect(smb.children[0]!.count).toBe(0);
  });

  it("carries the msf module type for launching", () => {
    const root = buildModuleTree({
      exploits: ["windows/smb/a"],
      payloads: ["windows/x64/meterpreter/reverse_tcp"],
      encoders: ["x86/singlebyte"],
      nops: ["x86/opty2"],
      evasion: ["windows/macro"],
    });
    expect(root.children.find(c => c.name === "exploits")!.type).toBe("exploit");
    expect(root.children.find(c => c.name === "payloads")!.type).toBe("payload");
    expect(root.children.find(c => c.name === "encoders")!.type).toBe("encoder");
    expect(root.children.find(c => c.name === "nops")!.type).toBe("nop");
    expect(root.children.find(c => c.name === "evasion")!.type).toBe("evasion");
  });

  it("leaf paths are bare refnames, rank-key and launch ready", () => {
    const root = buildModuleTree({ exploits: ["windows/smb/ms08_067_netapi"] });
    const smb = root.children[0]!.children[0]!.children[0]!;
    const leaf = smb.children[0]!;
    expect(leaf.path).toBe("windows/smb/ms08_067_netapi");
    expect(leaf.type).toBe("exploit");
  });

  it("sorts branches alphabetically", () => {
    const root = buildModuleTree({ nops: ["zzz/aaa", "x86/singlebyte"] });
    expect(root.children[0]!.children.map(c => c.name)).toEqual(["x86", "zzz"]);
  });

  it("keeps leaf and folder with the same name apart", () => {
    const root = buildModuleTree({ exploits: ["windows/a/b", "windows/a"] });
    const win = root.children[0]!.children[0]!;
    expect(win.children.map(c => c.name).sort()).toEqual(["a", "a"]);
    const leaf = win.children.find(c => c.children.length === 0)!;
    expect(leaf.path).toBe("windows/a");
  });

  it("ignores collections it does not know a module type for", () => {
    const root = buildModuleTree({ exploits: ["a/b"], sonic: ["boom"] } as Record<string, string[]>);
    expect(root.children.map(c => c.name)).toEqual(["exploits"]);
  });
});

describe("filterTree", () => {
  const root = buildModuleTree({
    exploits: ["windows/smb/ms17_010_eternalblue", "windows/http/rejetto_hfs"],
  });

  it("keeps branches containing matches and the matches themselves", () => {
    const keep = filterTree(root, "ms17_010");
    expect(keep.has("windows/smb/ms17_010_eternalblue")).toBe(true);
    expect(keep.has("exploit/windows/smb")).toBe(true);
    expect(keep.has("windows/http/rejetto_hfs")).toBe(false);
  });

  it("empty query keeps nothing", () => {
    expect(filterTree(root, "").size).toBe(0);
  });
});

describe("visibleChildren", () => {
  const root = buildModuleTree({
    exploits: ["windows/smb/a", "windows/smb/b", "multi/http/c"],
  });
  const windows = root.children[0]!.children[0]!;

  it("renders a collapsed branch's children not at all", () => {
    expect(visibleChildren(windows.children, false)).toEqual([]);
  });

  it("renders an expanded branch's children", () => {
    expect(visibleChildren(windows.children, true)).toEqual(windows.children);
  });
});
