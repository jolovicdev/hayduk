// msf option values must keep their types: rpc_execute rejects "443" where
// it wants an integer and "true" where it wants a boolean.

import { serviceOpen } from "./serviceState";

export interface LaunchService {
  host: string;
  port: number;
  name?: string;
  state?: string;
}

// Module paths carry their protocol in a segment (windows/smb/...,
// linux/http/...). These are the workspace service names each token can
// appear as, plus fallback ports for scanners that did not fingerprint a
// name. Matching the path rather than the module's prose keeps the guess
// predictable.
const SERVICE_TOKEN_NAMES: Record<string, string[]> = {
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

const SERVICE_TOKEN_PORTS: Record<string, number[]> = {
  ssh: [22], smb: [139, 445], ftp: [21], telnet: [23], smtp: [25],
  dns: [53], http: [80, 443, 591, 8000, 8080, 8443], mysql: [3306],
  mssql: [1433], postgres: [5432], rdp: [3389], vnc: [5900],
  ldap: [389], snmp: [161],
};

export function moduleServiceToken(name: string): string | undefined {
  const segments = name.toLowerCase().split("/");
  for (const token of Object.keys(SERVICE_TOKEN_NAMES)) {
    if (segments.includes(token)) return token;
  }
  return undefined;
}

// inferServicePort points a module at the port its service was actually
// found on instead of the module default. Explicit matches (Find attacks)
// pass their port directly and take precedence in the dialog.
export function inferServicePort(
  name: string,
  host: string | undefined,
  services: readonly (LaunchService | undefined)[],
): number | undefined {
  if (!host) return undefined;
  const token = moduleServiceToken(name);
  if (!token) return undefined;
  const names = SERVICE_TOKEN_NAMES[token] ?? [];
  const ports = SERVICE_TOKEN_PORTS[token] ?? [];
  const matches = services
    .filter((s): s is LaunchService => !!s && s.host === host && serviceOpen(s))
    .filter(s => {
      const serviceName = (s.name ?? "").toLowerCase();
      return (serviceName !== "" && names.some(n => serviceName.includes(n))) || ports.includes(s.port);
    })
    .map(s => s.port)
    .filter(port => port > 0)
    .sort((a, b) => a - b);
  return matches[0];
}

export interface OptionDef {
  type: string;
  required: boolean;
  enums?: string[];
  default?: unknown;
}

export type OptionKind = "text" | "number" | "bool" | "enum";

export function optionKind(o: OptionDef): OptionKind {
  if ((o.enums?.length ?? 0) > 0) return "enum";
  switch (o.type) {
    case "bool":
    case "boolean":
      return "bool";
    case "integer":
    case "int":
    case "port":
    case "numeric":
      return "number";
    default:
      return "text";
  }
}

export function defaultText(v: unknown): string {
  return v === undefined || v === null ? "" : String(v);
}

function numberValue(text: string): number | undefined {
  const n = Number(text);
  return Number.isFinite(n) ? n : undefined;
}

// collectOptions turns edit-state strings into typed values, dropping empty
// and unparseable entries.
export function collectOptions(
  defs: Record<string, OptionDef>,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, def] of Object.entries(defs)) {
    const kind = optionKind(def);
    const raw = values[name];
    if (kind === "bool") {
      if (typeof raw === "boolean") out[name] = raw;
      else if (raw === "true") out[name] = true;
      else if (raw === "false") out[name] = false;
      continue;
    }
    if (typeof raw !== "string") continue;
    const text = raw.trim();
    if (!text) continue;
    if (kind === "number") {
      const n = numberValue(text);
      if (n !== undefined) out[name] = n;
      continue;
    }
    out[name] = text;
  }
  return out;
}

// missingRequired names required options that resolve to no usable value
// (empty text, or a numeric field holding garbage). Defaults count.
export function missingRequired(
  defs: Record<string, OptionDef>,
  values: Record<string, unknown>,
): string[] {
  const missing: string[] = [];
  for (const [name, def] of Object.entries(defs)) {
    if (!def.required) continue;
    const raw = values[name];
    const hasDefault = def.default !== undefined && def.default !== null && def.default !== "";
    if (optionKind(def) === "bool") {
      if (typeof raw === "boolean" || raw === "true" || raw === "false" || hasDefault) continue;
      missing.push(name);
      continue;
    }
    const text = typeof raw === "string" ? raw.trim() : "";
    if (text) {
      if (optionKind(def) === "number" && numberValue(text) === undefined) missing.push(name);
      continue;
    }
    if (!hasDefault) missing.push(name);
  }
  return missing;
}

// The compatible-payloads request must identify the module the way every
// other module command does.
export function compatiblePayloadsParams(
  type: string,
  name: string,
): Record<string, string> | null {
  if (type !== "exploit") return null;
  return { type, name };
}

export function payloadGroup(payload: string): string {
  const slash = payload.indexOf("/");
  return slash > 0 ? payload.slice(0, slash) : "";
}

// missingLaunchOptions names the required options blocking a launch: the
// module's own, plus the chosen payload's once its settings are in hand.
export function missingLaunchOptions(o: {
  optionsLoading: boolean;
  optionsError: boolean;
  payloadChosen: boolean;
  payloadLoading: boolean;
  optsDefs: Record<string, OptionDef>;
  optsValues: Record<string, unknown>;
  payDefs: Record<string, OptionDef>;
  payValues: Record<string, unknown>;
}): string[] {
  if (o.optionsLoading || o.optionsError) return [];
  const missing = missingRequired(o.optsDefs, o.optsValues);
  if (o.payloadChosen && !o.payloadLoading) {
    missing.push(...missingRequired(o.payDefs, o.payValues));
  }
  return missing;
}

// The launch button's gate: no launch while either option set is still
// loading (it would send without defaults) or failed to load (it would send
// an empty option set), or while anything is missing.
export function launchDisabled(o: {
  busy: boolean;
  optionsLoading: boolean;
  optionsError: boolean;
  payloadChosen: boolean;
  payloadLoading: boolean;
  payloadError: boolean;
  missing: string[];
}): boolean {
  if (o.busy || o.optionsLoading || o.optionsError) return true;
  if (o.payloadChosen && (o.payloadLoading || o.payloadError)) return true;
  return o.missing.length > 0;
}
