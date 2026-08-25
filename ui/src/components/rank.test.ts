import { describe, expect, it } from "vitest";
import { rankChip } from "./rank";

describe("rankChip", () => {
  it("highlights excellent and great", () => {
    expect(rankChip("excellent")).toEqual({ label: "excellent", hot: true });
    expect(rankChip("great")).toEqual({ label: "great", hot: true });
    expect(rankChip("Great")).toEqual({ label: "great", hot: true });
  });

  it("dims the middling ranks", () => {
    expect(rankChip("good")).toEqual({ label: "good", hot: false });
    expect(rankChip("manual")).toEqual({ label: "manual", hot: false });
  });

  it("stays quiet for normal and unknown", () => {
    expect(rankChip("normal")).toBeNull();
    expect(rankChip("")).toBeNull();
    expect(rankChip(undefined)).toBeNull();
  });
});
