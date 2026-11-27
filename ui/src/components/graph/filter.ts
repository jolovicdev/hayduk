import type { CredState, HostState, ServiceState, SessionState } from "../../protocol/types";
import { detectOsFamily, type OsFamily } from "../OsIcon";
import { serviceOpen } from "../../views/serviceState";

export type AccessFilter = "all" | "sessions" | "creds" | "none";
export type OsFilter = "all" | OsFamily;

export interface TopologyFilter {
  query: string;
  access: AccessFilter;
  os: OsFilter;
}

export const EMPTY_TOPOLOGY_FILTER: TopologyFilter = {
  query: "",
  access: "all",
  os: "all",
};

export function isTopologyFilterActive(f: TopologyFilter): boolean {
  return f.query.trim() !== "" || f.access !== "all" || f.os !== "all";
}

function hostMatchesTerm(
  host: HostState,
  services: readonly ServiceState[],
  term: string,
): boolean {
  const portTerm = term.startsWith("port:") ? term.slice(5) : term;
  if (/^\d+$/.test(portTerm)) {
    const port = Number(portTerm);
    if (services.some(s => serviceOpen(s) && s.port === port)) return true;
  }
  const fields = [host.address, host.name, host.osName, host.osFlavor, host.osVersion, host.comments];
  for (const field of fields) {
    if (field && field.toLowerCase().includes(term)) return true;
  }
  for (const s of services) {
    if (!serviceOpen(s)) continue;
    if (s.name.toLowerCase().includes(term) || s.info.toLowerCase().includes(term)) return true;
    if (`${s.port}/${s.proto}`.toLowerCase().includes(term)) return true;
  }
  return false;
}

// filterTopologyHosts implements Dynamic Workspaces on the client: slice the
// topology graph by access status, OS family, subnet, port, or service text
// without altering the underlying Metasploit database workspace.
export function filterTopologyHosts(
  hosts: readonly HostState[],
  servicesByHost: ReadonlyMap<string, readonly ServiceState[]>,
  sessionsByHost: ReadonlyMap<string, readonly SessionState[]>,
  credsByHost: ReadonlyMap<string, readonly CredState[]>,
  filter: TopologyFilter,
): HostState[] {
  if (!isTopologyFilterActive(filter)) return [...hosts];
  const terms = filter.query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return hosts.filter(host => {
    const hasSession = (sessionsByHost.get(host.address)?.length ?? 0) > 0;
    const hasCreds = (credsByHost.get(host.address)?.length ?? 0) > 0;
    if (filter.access === "sessions" && !hasSession) return false;
    if (filter.access === "creds" && !hasCreds) return false;
    if (filter.access === "none" && (hasSession || hasCreds)) return false;

    if (filter.os !== "all" && detectOsFamily(host) !== filter.os) return false;

    if (terms.length > 0) {
      const svcs = servicesByHost.get(host.address) ?? [];
      for (const term of terms) {
        if (!hostMatchesTerm(host, svcs, term)) return false;
      }
    }
    return true;
  });
}
