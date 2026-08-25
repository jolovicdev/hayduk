// msfrpcd lists module refnames without the type prefix ("scanner/...", not
// "auxiliary/scanner/..."), and module.execute accepts that relative form.
export const DISCOVERY_MODULES = [
  "scanner/discovery/udp_sweep",
  "scanner/discovery/udp_probe",
  "scanner/discovery/arp_sweep",
  "scanner/portscan/tcp",
];

export const SERVICE_MODULES = [
  "scanner/portscan/tcp",
  "scanner/portscan/syn",
  "scanner/portscan/ack",
  "scanner/portscan/xmas",
  "scanner/portscan/ftpbounce",
];

// Keep only modules the connected framework actually carries, preserving the
// curated preference order. An empty result means the curated list is stale
// for this install.
export function pickModules(
  available: readonly string[] | undefined,
  curated: readonly string[],
): string[] {
  if (!available) return [];
  const have = new Set(available);
  return curated.filter(m => have.has(m));
}
