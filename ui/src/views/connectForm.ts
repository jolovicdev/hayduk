// Connect-dialog form state, pure so the port edge cases stay tested.

export const CONNECT_DEFAULTS = { host: "127.0.0.1", port: 55553, ssl: false, username: "msf" };

export function parsePort(text: string): number | undefined {
  if (!/^\d+$/.test(text.trim())) return undefined;
  const n = Number(text.trim());
  return n >= 1 && n <= 65535 ? n : undefined;
}

export function loadConnectDefaults(
  storage: Pick<Storage, "getItem"> = localStorage,
): typeof CONNECT_DEFAULTS {
  const d = { ...CONNECT_DEFAULTS };
  for (const k of Object.keys(CONNECT_DEFAULTS) as (keyof typeof CONNECT_DEFAULTS)[]) {
    const v = storage.getItem("hayduk." + k);
    if (v === null) continue;
    if (k === "port") {
      const p = parsePort(v);
      if (p !== undefined) (d as any)[k] = p;
    } else if (k === "ssl") {
      (d as any)[k] = v === "true";
    } else {
      (d as any)[k] = v;
    }
  }
  return d;
}

// Preserve the original error as detail when a known error has user guidance.
export function friendlyConnectError(raw: string): { primary: string; detail: string } {
  const text = raw.trim();
  const lower = text.toLowerCase();
  if (lower.includes("login failed")) {
    return { primary: "Login failed - msfrpcd rejected the username or password.", detail: text };
  }
  if (/connection refused|no connection could be made/.test(lower)) {
    return { primary: "Connection refused - nothing is answering at that host and port; is msfrpcd running?", detail: text };
  }
  if (/timeout|timed out|deadline exceeded/.test(lower)) {
    return { primary: "Timed out reaching msfrpcd - check the host, port, and SSL setting.", detail: text };
  }
  if (/tls|ssl|handshake|http response to https/.test(lower)) {
    return { primary: "Protocol mismatch - try toggling the SSL option to match how msfrpcd runs.", detail: text };
  }
  return { primary: text, detail: "" };
}
