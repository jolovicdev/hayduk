// One protocol -> service-name/port table shared by brute force and launch
// prefill. Keeping these in one place means a new alias cannot work in one
// workflow and silently miss in the other.

import { serviceOpen } from "./serviceState";

export interface ServiceLike {
  host?: string;
  port?: number;
  name?: string;
  state?: string;
}

export const SERVICE_NAMES: Record<string, string[]> = {
  ssh: ["ssh"],
  smb: ["smb", "microsoft-ds", "netbios-ssn"],
  ftp: ["ftp"],
  telnet: ["telnet"],
  smtp: ["smtp"],
  dns: ["dns", "domain"],
  http: ["http", "http-proxy", "https", "ssl/http"],
  mysql: ["mysql", "ssl/mysql"],
  mssql: ["mssql", "ms-sql-s", "ms-sql-m"],
  postgres: ["postgres", "postgresql"],
  rdp: ["rdp", "ms-wbt-server"],
  vnc: ["vnc"],
  ldap: ["ldap"],
  snmp: ["snmp"],
};

export const SERVICE_PORTS: Record<string, number[]> = {
  ssh: [22], smb: [139, 445], ftp: [21], telnet: [23], smtp: [25],
  dns: [53], http: [80, 443, 591, 8000, 8080, 8443], mysql: [3306],
  mssql: [1433], postgres: [5432], rdp: [3389], vnc: [5900],
  ldap: [389], snmp: [161],
};

export function serviceMatches(
  service: ServiceLike | undefined,
  names: readonly string[],
  ports: readonly number[],
): boolean {
  if (!service || !serviceOpen(service)) return false;
  const name = (service.name ?? "").toLowerCase();
  if (name !== "" && names.some(n => name.includes(n))) return true;
  return ports.includes(service.port ?? -1);
}

// matchingServicePort returns the lowest open port carrying one of the
// protocol's names, falling back to the well-known ports when the scanner
// did not fingerprint a name.
export function matchingServicePort(
  host: string,
  names: readonly string[],
  ports: readonly number[],
  services: readonly (ServiceLike | undefined)[],
): number | undefined {
  const found = services
    .filter((s): s is ServiceLike => !!s && s.host === host && serviceMatches(s, names, ports))
    .map(s => s.port ?? 0)
    .filter(port => port > 0)
    .sort((a, b) => a - b);
  return found[0];
}
