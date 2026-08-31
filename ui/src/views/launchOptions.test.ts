import { describe, expect, it } from "vitest";
import {
  collectOptions, compatiblePayloadsParams, launchDisabled, missingLaunchOptions, missingRequired, optionKind,
} from "./launchOptions";

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

describe("compatiblePayloadsParams", () => {
  it("identifies the module by type and name", () => {
    expect(compatiblePayloadsParams("exploit", "windows/smb/psexec"))
      .toEqual({ type: "exploit", name: "windows/smb/psexec" });
  });

  it("asks for nothing on non-exploits", () => {
    expect(compatiblePayloadsParams("auxiliary", "scanner/discovery/udp_probe")).toBeNull();
  });
});

describe("missingLaunchOptions", () => {
  const base = {
    optionsLoading: false, optionsError: false,
    optsDefs: { RHOSTS: { type: "string", required: true } },
    optsValues: { RHOSTS: "10.0.0.5" },
    payDefs: { LHOST: { type: "string", required: true } },
    payValues: {},
  };

  it("names required payload options without values", () => {
    expect(missingLaunchOptions({ ...base, payloadChosen: true, payloadLoading: false }))
      .toEqual(["LHOST"]);
  });

  it("stays quiet about the payload until one is chosen", () => {
    expect(missingLaunchOptions({ ...base, payloadChosen: false, payloadLoading: false }))
      .toEqual([]);
  });

  it("names module options and payload options together", () => {
    expect(missingLaunchOptions({
      ...base,
      optsValues: {},
      payloadChosen: true, payloadLoading: false,
    })).toEqual(["RHOSTS", "LHOST"]);
  });
});

describe("launchDisabled", () => {
  it("holds the launch while payload settings load", () => {
    expect(launchDisabled({
      busy: false, optionsLoading: false, optionsError: false,
      payloadChosen: true, payloadLoading: true, payloadError: false, missing: [],
    })).toBe(true);
  });

  it("holds the launch when payload options failed to load", () => {
    // an enabled button would send the payload with no options; required
    // ones like LHOST would be rejected with no way to enter them
    expect(launchDisabled({
      busy: false, optionsLoading: false, optionsError: false,
      payloadChosen: true, payloadLoading: false, payloadError: true, missing: [],
    })).toBe(true);
  });

  it("launches once everything is loaded and complete", () => {
    expect(launchDisabled({
      busy: false, optionsLoading: false, optionsError: false,
      payloadChosen: true, payloadLoading: false, payloadError: false, missing: [],
    })).toBe(false);
  });

  it("holds the launch while module options load", () => {
    expect(launchDisabled({
      busy: false, optionsLoading: true, optionsError: false,
      payloadChosen: false, payloadLoading: false, payloadError: false, missing: [],
    })).toBe(true);
  });

  it("holds the launch when module options failed to load", () => {
    // a failed options load leaves nothing to gate on; an enabled button
    // would submit an empty option set
    expect(launchDisabled({
      busy: false, optionsLoading: false, optionsError: true,
      payloadChosen: false, payloadLoading: false, payloadError: false, missing: [],
    })).toBe(true);
  });
});
