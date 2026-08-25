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
