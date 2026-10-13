import { describe, expect, it } from "vitest";
import { attemptOptions, buildBrutePlan, parseLines } from "./brute";

describe("parseLines", () => {
  it("trims, drops empties and dedupes, keeping order", () => {
    expect(parseLines("  admin \n\nuser\nadmin\n\ntest  ")).toEqual(["admin", "user", "test"]);
    expect(parseLines("")).toEqual([]);
    expect(parseLines("\n \n")).toEqual([]);
  });
});

describe("buildBrutePlan", () => {
  it("is the cartesian product of hosts, users and passes", () => {
    const plan = buildBrutePlan({
      hosts: ["10.0.0.1", "10.0.0.2"], users: ["admin", "user"], passes: ["pw", "123"],
      module: "scanner/smb/smb_login",
    });
    expect(plan.attempts).toHaveLength(8);
    expect(plan.attempts[0]).toEqual({ host: "10.0.0.1", user: "admin", pass: "pw" });
    expect(plan.attempts[7]).toEqual({ host: "10.0.0.2", user: "user", pass: "123" });
    expect(plan.capped).toBe(0);
  });

  it("adds a blank pass guess once per user when asked", () => {
    const plan = buildBrutePlan({
      hosts: ["h"], users: ["admin"], passes: ["pw"], addBlank: true,
      module: "scanner/smb/smb_login",
    });
    expect(plan.attempts.map(a => a.pass)).toEqual(["pw", ""]);
  });

  it("does not add a blank pass guess twice", () => {
    const plan = buildBrutePlan({
      hosts: ["h"], users: ["admin"], passes: [""], addBlank: true,
      module: "scanner/smb/smb_login",
    });
    expect(plan.attempts).toHaveLength(1);
  });

  it("adds a user-as-pass guess unless the pass list already has it", () => {
    const withIt = buildBrutePlan({
      hosts: ["h"], users: ["admin"], passes: ["pw"], addUserAsPass: true,
      module: "scanner/ssh/ssh_login",
    });
    expect(withIt.attempts.map(a => a.pass)).toEqual(["pw", "admin"]);

    const already = buildBrutePlan({
      hosts: ["h"], users: ["admin"], passes: ["admin"], addUserAsPass: true,
      module: "scanner/ssh/ssh_login",
    });
    expect(already.attempts.map(a => a.pass)).toEqual(["admin"]);
  });

  it("ignores the user list for userless scanners", () => {
    const plan = buildBrutePlan({
      hosts: ["h"], users: ["a", "b"], passes: ["pw"], userless: true,
      module: "scanner/vnc/vnc_login",
    });
    expect(plan.attempts).toEqual([{ host: "h", user: "", pass: "pw" }]);
  });

  it("caps the plan and counts what it dropped", () => {
    const plan = buildBrutePlan({
      hosts: ["h"], users: ["a", "b", "c"], passes: ["1", "2", "3", "4"],
      module: "scanner/smb/smb_login", cap: 5,
    });
    expect(plan.attempts).toHaveLength(5);
    expect(plan.capped).toBe(7);
  });
});

describe("attemptOptions", () => {
  it("sends only options every login scanner carries", () => {
    expect(attemptOptions({ host: "h", user: "u", pass: "p" })).toEqual({
      RHOSTS: "h", THREADS: 1, USERNAME: "u", PASSWORD: "p",
    });
    expect(attemptOptions({ host: "h", user: "", pass: "p" }, true)).toEqual({
      RHOSTS: "h", THREADS: 1, PASSWORD: "p",
    });
  });
});
