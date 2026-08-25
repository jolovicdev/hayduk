import { describe, expect, it } from "vitest";
import { ageOf, interactPrompt } from "./format";

describe("ageOf", () => {
  it("formats seconds, minutes and hours", () => {
    const now = Date.now();
    expect(ageOf(new Date(now - 5_000).toISOString(), now)).toBe("5s");
    expect(ageOf(new Date(now - 90_000).toISOString(), now)).toBe("1m");
    expect(ageOf(new Date(now - 7_200_000).toISOString(), now)).toBe("2h");
  });

  it("never renders NaN for missing or garbage timestamps", () => {
    expect(ageOf(undefined, Date.now())).toBe("-");
    expect(ageOf("", Date.now())).toBe("-");
    expect(ageOf("not-a-date", Date.now())).toBe("-");
  });

  it("clock skew from the server clamps to zero", () => {
    expect(ageOf(new Date(Date.now() + 60_000).toISOString(), Date.now())).toBe("0s");
  });
});

describe("interactPrompt", () => {
  it("meterpreter sessions get the meterpreter prompt", () => {
    expect(interactPrompt("meterpreter", "4")).toBe("meterpreter 4 > ");
  });

  it("shell sessions must not claim to be meterpreter", () => {
    expect(interactPrompt("shell", "7")).toBe("7 sh > ");
    expect(interactPrompt("", "7")).toBe("7 sh > ");
  });
});
