// Brute-force planning: the login scanners take one credential pair per
// run, so the dialog sprays pairs and the cartesian product is built here.

import { SERVICE_NAMES, SERVICE_PORTS, matchingServicePort, serviceMatches } from "./serviceCatalog";

export interface BruteAttempt {
  host: string;
  user: string;
  pass: string;
  // port is the scanned service port for this host. 0/undefined omits RPORT
  // so the module's default applies.
  port?: number;
}

export interface BrutePlan {
  module: string;
  attempts: BruteAttempt[];
  // attempts the cap dropped; the dialog reports them instead of silently
  // spraying fewer pairs than the operator asked for
  capped: number;
}

export interface LoginScanner {
  service: string;
  label: string;
  module: string;
  // names is how the workspace service row may spell this protocol; ports is
  // the fallback when the scanner did not name the service.
  names: string[];
  ports: number[];
  // vnc authenticates on the password alone; its attempts ignore the user
  // list rather than spraying duplicates
  userless?: boolean;
}

// curated like scan.ts: kept to scanners the framework actually carries,
// in preference order
export const LOGIN_SCANNERS: LoginScanner[] = [
  {
    service: "smb", label: "SMB", module: "scanner/smb/smb_login",
    names: SERVICE_NAMES.smb!, ports: SERVICE_PORTS.smb!,
  },
  {
    service: "ssh", label: "SSH", module: "scanner/ssh/ssh_login",
    names: SERVICE_NAMES.ssh!, ports: SERVICE_PORTS.ssh!,
  },
  {
    service: "ftp", label: "FTP", module: "scanner/ftp/ftp_login",
    names: SERVICE_NAMES.ftp!, ports: SERVICE_PORTS.ftp!,
  },
  {
    service: "telnet", label: "Telnet", module: "scanner/telnet/telnet_login",
    names: SERVICE_NAMES.telnet!, ports: SERVICE_PORTS.telnet!,
  },
  {
    service: "mssql", label: "MSSQL", module: "scanner/mssql/mssql_login",
    names: SERVICE_NAMES.mssql!, ports: SERVICE_PORTS.mssql!,
  },
  {
    service: "mysql", label: "MySQL", module: "scanner/mysql/mysql_login",
    names: SERVICE_NAMES.mysql!, ports: SERVICE_PORTS.mysql!,
  },
  {
    service: "postgres", label: "PostgreSQL", module: "scanner/postgres/postgres_login",
    names: SERVICE_NAMES.postgres!, ports: SERVICE_PORTS.postgres!,
  },
  {
    service: "vnc", label: "VNC", module: "scanner/vnc/vnc_login",
    names: SERVICE_NAMES.vnc!, ports: SERVICE_PORTS.vnc!, userless: true,
  },
];

export const BRUTE_CAP = 250;

// parseLines reads one-per-line textarea input: trimmed, empties dropped,
// duplicates removed, order kept. A Set keeps a large paste linear.
export function parseLines(text: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line !== "" && !seen.has(line)) {
      seen.add(line);
      out.push(line);
    }
  }
  return out;
}

export interface BruteService {
  host: string;
  port: number;
  name: string;
  state?: string;
}

export function matchesLoginScanner(
  service: { port?: number; name?: string; state?: string },
  scanner: LoginScanner,
): boolean {
  return serviceMatches(service, scanner.names, scanner.ports);
}

// defaultScannerForHost preselects the login scanner that matches the
// selected host's open services, instead of always starting on SMB.
export function defaultScannerForHost(
  host: string | undefined,
  services: readonly (BruteService | undefined)[],
): LoginScanner | undefined {
  if (!host) return undefined;
  return LOGIN_SCANNERS.find(s =>
    services.some(svc => !!svc && svc.host === host && matchesLoginScanner(svc, s)));
}

// servicePortForHost returns the lowest open port carrying the scanner's
// service. The scan is the source of truth; a module default is only used
// when the workspace has no matching row.
export function servicePortForHost(
  host: string,
  scanner: LoginScanner,
  services: readonly (BruteService | undefined)[],
): number | undefined {
  return matchingServicePort(host, scanner.names, scanner.ports, services);
}

export interface BruteInput {
  hosts: string[];
  users: string[];
  passes: string[];
  module: string;
  userless?: boolean;
  addBlank?: boolean;
  addUserAsPass?: boolean;
  cap?: number;
  // port overrides every host when set; ports maps a scanned port per host.
  port?: number;
  ports?: Record<string, number | undefined>;
}

export function buildBrutePlan(input: BruteInput): BrutePlan {
  const cap = input.cap ?? BRUTE_CAP;
  // a blank password is one entry in the pass list; a user-as-pass guess
  // depends on the user, so it is added per pair below
  const passes = [...input.passes];
  if (input.addBlank && !passes.includes("")) passes.push("");
  const users = input.userless ? [""] : input.users;
  const attempts: BruteAttempt[] = [];
  let capped = 0;
  const push = (host: string, user: string, pass: string) => {
    if (attempts.length >= cap) { capped++; return; }
    const port = input.port ?? input.ports?.[host];
    attempts.push({ host, user, pass, ...(port ? { port } : {}) });
  };
  for (const host of input.hosts) {
    for (const user of users) {
      for (const pass of passes) push(host, user, pass);
      if (input.addUserAsPass && user !== "" && !passes.includes(user)) {
        push(host, user, user);
      }
    }
  }
  return { module: input.module, attempts, capped };
}

// attemptOptions is the module.execute options map for one pair. Only
// options every login scanner carries are sent: msf validates each name
// and rejects extras by option name. RPORT is always valid for scanners.
export function attemptOptions(a: BruteAttempt, userless?: boolean): Record<string, unknown> {
  const options: Record<string, unknown> = { RHOSTS: a.host, THREADS: 1 };
  if (a.port) options.RPORT = a.port;
  if (!userless) options.USERNAME = a.user;
  options.PASSWORD = a.pass;
  return options;
}
