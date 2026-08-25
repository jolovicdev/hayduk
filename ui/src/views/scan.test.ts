import { describe, expect, it } from "vitest";
import { DISCOVERY_MODULES, SERVICE_MODULES, pickModules } from "./scan";

describe("pickModules", () => {
  it("keeps curated order and drops modules this install lacks", () => {
    const available = ["scanner/portscan/syn", "scanner/discovery/udp_sweep", "other/module"];
    expect(pickModules(available, DISCOVERY_MODULES)).toEqual(["scanner/discovery/udp_sweep"]);
    expect(pickModules(available, SERVICE_MODULES)).toEqual(["scanner/portscan/syn"]);
  });

  it("returns empty without a module index", () => {
    expect(pickModules(undefined, DISCOVERY_MODULES)).toEqual([]);
    expect(pickModules([], DISCOVERY_MODULES)).toEqual([]);
  });

  it("curated scanner names are real metasploit refnames", () => {
    // a typo here silently removes the scanner from every install
    const known = new Set([
      "scanner/discovery/udp_sweep", "scanner/discovery/udp_probe", "scanner/discovery/arp_sweep",
      "scanner/portscan/tcp", "scanner/portscan/syn", "scanner/portscan/ack",
      "scanner/portscan/xmas", "scanner/portscan/ftpbounce",
    ]);
    for (const m of [...DISCOVERY_MODULES, ...SERVICE_MODULES]) {
      expect(known.has(m), m).toBe(true);
    }
  });
});
