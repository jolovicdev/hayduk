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

export interface FileRow {
  name: string;
  size: string;
  kind: string;
}

export function parseFiles(output: string): FileRow[] {
  const rows: FileRow[] = [];
  let inTable = false;
  for (const line of output.split("\n")) {
    if (/^-{3,}/.test(line.trim())) {
      inTable = true;
      continue;
    }
    if (!inTable) continue;
    const parts = line.trim().split(/\s+/);
    if (parts.length < 5) continue;
    // Mode Size Type Date Time Zone Name... — the modified column spans
    // three fields, so the name starts after the date
    const dateIdx = parts.findIndex(p => /^\d{4}-\d{2}-\d{2}$/.test(p));
    const name = dateIdx >= 0 ? parts.slice(dateIdx + 3).join(" ") : parts.slice(4).join(" ");
    if (!name || name === "Name") continue;
    rows.push({ kind: parts[2]!, size: parts[1]!, name });
  }
  return rows;
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
