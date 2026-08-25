import { describe, expect, it } from "vitest";
import { collectOptions, missingRequired, optionKind } from "./launchOptions";

const defs = {
  RHOSTS: { type: "string", required: true },
  RPORT: { type: "port", required: false, default: 445 },
  VERBOSE: { type: "bool", required: false, default: true },
  ACTION: { type: "enum", required: false, enums: ["SCAN", "EXPLOIT"] },
  CHUNK_SIZE: { type: "integer", required: false, default: 1024 },
} as Record<string, any>;

describe("optionKind", () => {
  it("picks the right editor per msf option type", () => {
    expect(optionKind(defs.RHOSTS)).toBe("text");
    expect(optionKind(defs.RPORT)).toBe("number");
    expect(optionKind(defs.CHUNK_SIZE)).toBe("number");
    expect(optionKind(defs.VERBOSE)).toBe("bool");
    expect(optionKind(defs.ACTION)).toBe("enum");
    expect(optionKind({ type: "string", required: true, enums: [] })).toBe("text");
  });
});

describe("collectOptions", () => {
  it("keeps types: numbers as numbers, bools as booleans", () => {
    const values = { RHOSTS: "10.0.0.5", RPORT: "445", VERBOSE: false, CHUNK_SIZE: "2048" };
    expect(collectOptions(defs, values)).toEqual({
      RHOSTS: "10.0.0.5",
      RPORT: 445,
      VERBOSE: false,
      CHUNK_SIZE: 2048,
    });
  });

  it("omits empty and invalid entries", () => {
    expect(collectOptions(defs, { RPORT: "", VERBOSE: true, CHUNK_SIZE: "abc" })).toEqual({ VERBOSE: true });
  });

  it("accepts boolean defaults untouched", () => {
    expect(collectOptions(defs, {})).toEqual({});
    expect(collectOptions({ B: { type: "bool", required: false, default: false } }, {}).B).toBeUndefined();
  });
});

describe("missingRequired", () => {
  it("lists required options that resolved to nothing", () => {
    const d = { RHOSTS: { type: "string", required: true }, THREADS: { type: "integer", required: true, default: 1 } };
    expect(missingRequired(d, {})).toEqual(["RHOSTS"]); // defaults satisfy THREADS
    expect(missingRequired(d, { RHOSTS: "10.0.0.1" })).toEqual([]);
  });

  it("a number option with garbage text counts as missing", () => {
    const d = { PORT: { type: "port", required: true } };
    expect(missingRequired(d, { PORT: "zz" })).toEqual(["PORT"]);
    expect(missingRequired(d, { PORT: "4444" })).toEqual([]);
  });
});
