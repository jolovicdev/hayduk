// Brute-force planning: the login scanners take one credential pair per
// run, so the dialog sprays pairs and the cartesian product is built here.

export interface BruteAttempt {
  host: string;
  user: string;
  pass: string;
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
  // vnc authenticates on the password alone; its attempts ignore the user
  // list rather than spraying duplicates
  userless?: boolean;
}

// curated like scan.ts: kept to scanners the framework actually carries,
// in preference order
export const LOGIN_SCANNERS: LoginScanner[] = [
  { service: "smb", label: "SMB", module: "scanner/smb/smb_login" },
  { service: "ssh", label: "SSH", module: "scanner/ssh/ssh_login" },
  { service: "ftp", label: "FTP", module: "scanner/ftp/ftp_login" },
  { service: "telnet", label: "Telnet", module: "scanner/telnet/telnet_login" },
  { service: "mssql", label: "MSSQL", module: "scanner/mssql/mssql_login" },
  { service: "mysql", label: "MySQL", module: "scanner/mysql/mysql_login" },
  { service: "postgres", label: "PostgreSQL", module: "scanner/postgres/postgres_login" },
  { service: "vnc", label: "VNC", module: "scanner/vnc/vnc_login", userless: true },
];

export const BRUTE_CAP = 250;

// parseLines reads one-per-line textarea input: trimmed, empties dropped,
// duplicates removed, order kept.
export function parseLines(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line !== "" && !out.includes(line)) out.push(line);
  }
  return out;
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
    attempts.push({ host, user, pass });
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
// and rejects extras by option name.
export function attemptOptions(a: BruteAttempt, userless?: boolean): Record<string, unknown> {
  const options: Record<string, unknown> = { RHOSTS: a.host, THREADS: 1 };
  if (!userless) options.USERNAME = a.user;
  options.PASSWORD = a.pass;
  return options;
}
