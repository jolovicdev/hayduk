// Parsers for meterpreter tool output. The engine returns the raw text of
// one command; these pull tables out of the two listings worth rendering.

export interface ProcessRow {
  pid: string;
  ppid: string;
  name: string;
  detail: string;
}

export function parseProcesses(output: string): ProcessRow[] {
  const rows: ProcessRow[] = [];
  for (const line of output.split("\n")) {
    const m = /^\s*(\d+)\s+(\d+)\s+(\S+)\s*(.*)$/.exec(line);
    if (!m) continue;
    if (m[3] === "Name" || /^-+$/.test(m[3]!)) continue;
    rows.push({ pid: m[1]!, ppid: m[2]!, name: m[3]!, detail: (m[4] ?? "").trim() });
  }
  return rows;
}

// joinRemote and parentRemote navigate the session's filesystem with the
// separator the remote OS reported, not the one Hayduk runs on.
export function joinRemote(dir: string, name: string, sep: string): string {
  if (!dir) return name;
  return dir.endsWith(sep) ? dir + name : dir + sep + name;
}

export function parentRemote(path: string, sep: string): string {
  if (/^[A-Za-z]:[\\/]?$/.test(path)) {
    return path.length === 2 ? path + sep : path;
  }
  const trimmed = path.endsWith(sep) && path.length > sep.length
    ? path.slice(0, -sep.length)
    : path;
  const idx = trimmed.lastIndexOf(sep);
  if (idx < 0) return ".";
  if (idx === 0) return sep;
  const head = trimmed.slice(0, idx);
  if (/^[A-Za-z]:$/.test(head)) return head + sep;
  return head;
}

export interface PortForwardRow {
  local: string;
  remote: string;
}

export function parsePortForwards(output: string): PortForwardRow[] {
  const rows: PortForwardRow[] = [];
  for (const line of output.split("\n")) {
    const m = /(\d{1,3}(?:\.\d{1,3}){3}:\d+|\*:\d+)\s*(?:->|=>|→)\s*(\S+)/.exec(line);
    if (m) rows.push({ local: m[1]!, remote: m[2]! });
    const idx = /^\s*\d+\s+(\S+)\s+(\S+)\s*$/.exec(line);
    if (!m && idx && /:\d+$/.test(idx[1]!) && /:\d+$/.test(idx[2]!)) {
      rows.push({ local: idx[1]!, remote: idx[2]! });
    }
  }
  return rows;
}
