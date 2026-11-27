import { describe, expect, it } from "vitest";
import type { CredState, HostState, ServiceState, SessionState } from "../../protocol/types";
import { EMPTY_TOPOLOGY_FILTER, filterTopologyHosts, isTopologyFilterActive } from "./filter";

function host(address: string, patch: Partial<HostState> = {}): HostState {
  return {
    address, name: "", mac: "", osName: "", osFlavor: "", osVersion: "",
    purpose: "", info: "", comments: "", ...patch,
  };
}

function svc(hostAddr: string, port: number, name: string, state = "open", info = ""): ServiceState {
  return { host: hostAddr, port, proto: "tcp", name, state, info };
}

describe("filterTopologyHosts", () => {
  const hosts = [
    host("10.0.1.10", { name: "dc01", osName: "Windows Server 2022" }),
    host("10.0.1.20", { name: "web01", osName: "Ubuntu Linux" }),
    host("10.0.2.30", { name: "db01", osName: "Linux", comments: "legacy postgres" }),
  ];
  const services = new Map<string, ServiceState[]>([
    ["10.0.1.10", [svc("10.0.1.10", 445, "smb"), svc("10.0.1.10", 3389, "rdp", "closed")]],
    ["10.0.1.20", [svc("10.0.1.20", 80, "http", "open", "nginx 1.24"), svc("10.0.1.20", 22, "ssh")]],
    ["10.0.2.30", [svc("10.0.2.30", 5432, "postgresql")]],
  ]);
  const sessions = new Map<string, SessionState[]>([
    ["10.0.1.20", [{ id: "1", type: "meterpreter", targetHost: "10.0.1.20" }]],
  ]);
  const creds = new Map<string, CredState[]>([
    ["10.0.1.10", [{ host: "10.0.1.10", port: 445, proto: "tcp", service: "smb", user: "admin", pass: "p", type: "password" }]],
  ]);

  it("returns all hosts when no filter is active", () => {
    expect(isTopologyFilterActive(EMPTY_TOPOLOGY_FILTER)).toBe(false);
    expect(filterTopologyHosts(hosts, services, sessions, creds, EMPTY_TOPOLOGY_FILTER).map(h => h.address))
      .toEqual(["10.0.1.10", "10.0.1.20", "10.0.2.30"]);
  });

  it("filters by access state", () => {
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, access: "sessions" }).map(h => h.address))
      .toEqual(["10.0.1.20"]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, access: "creds" }).map(h => h.address))
      .toEqual(["10.0.1.10"]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, access: "none" }).map(h => h.address))
      .toEqual(["10.0.2.30"]);
  });

  it("filters by OS family", () => {
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, os: "win" }).map(h => h.address))
      .toEqual(["10.0.1.10"]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, os: "linux" }).map(h => h.address))
      .toEqual(["10.0.1.20", "10.0.2.30"]);
  });

  it("matches open ports, services, subnet prefixes, and multi-term queries while ignoring closed ports", () => {
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, query: "445" }).map(h => h.address))
      .toEqual(["10.0.1.10"]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, query: "3389" }))
      .toEqual([]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, query: "10.0.1. nginx" }).map(h => h.address))
      .toEqual(["10.0.1.20"]);
    expect(filterTopologyHosts(hosts, services, sessions, creds, { ...EMPTY_TOPOLOGY_FILTER, query: "port:5432 legacy" }).map(h => h.address))
      .toEqual(["10.0.2.30"]);
  });
});
