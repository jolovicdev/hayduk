import { describe, expect, it } from "vitest";
import { parsePrompt } from "./console";

describe("parsePrompt", () => {
  it("parses the base framework prompt", () => {
    expect(parsePrompt("banner\nmsf6 > ")).toEqual({ value: "msf6 > ", start: 7 });
  });

  it("parses a module prompt", () => {
    const output = "loaded\nmsf6 exploit(windows/smb/ms17_010_eternalblue) > ";
    expect(parsePrompt(output)).toEqual({
      value: "msf6 exploit(windows/smb/ms17_010_eternalblue) > ",
      start: 7,
    });
  });

  it("accepts prompts without a trailing space", () => {
    expect(parsePrompt("msf >")).toEqual({ value: "msf >", start: 0 });
  });

  it("ignores prompts followed by command output", () => {
    expect(parsePrompt("msf6 > run\nstarted\n")).toBeNull();
  });

  it("ignores unrelated angle brackets", () => {
    expect(parsePrompt("request completed > ")).toBeNull();
  });
});
