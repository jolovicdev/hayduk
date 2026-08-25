import type { MenuItem } from "../components/contextmenu";

// Pivot routing runs entirely through the autoroute post module; the engine
// needs no new command, and its route poller (kicked when the autoroute job
// ends) is what carries the change back into the topology.
export const AUTOROUTE_MODULE = "multi/manage/autoroute";

export interface RouteTarget {
  subnet: string;
  netmask?: string;
}

// prefixToNetmask turns CIDR bits into the dotted quad the module's
// OptAddress NETMASK option demands ("/24" would fail datastore validation).
export function prefixToNetmask(bits: number): string | undefined {
  if (!Number.isInteger(bits) || bits < 0 || bits > 32) return undefined;
  const octet = (filled: number) =>
    filled >= 8 ? 255 : filled <= 0 ? 0 : 256 - 2 ** (8 - filled);
  return [0, 8, 16, 24].map(o => octet(bits - o)).join(".");
}

// parseRouteTarget splits a polled "a.b.c.d/prefix" subnet back into the
// SUBNET/NETMASK pair removal needs. Without a parseable IPv4 prefix the
// subnet is kept verbatim and the netmask omitted.
export function parseRouteTarget(subnet: string): RouteTarget {
  const slash = subnet.lastIndexOf("/");
  if (slash < 0) return { subnet };
  const prefix = Number(subnet.slice(slash + 1));
  const netmask = prefixToNetmask(prefix);
  if (netmask === undefined) return { subnet };
  return { subnet: subnet.slice(0, slash), netmask };
}

export function isIPv4(value: string): boolean {
  const parts = value.trim().split(".");
  if (parts.length !== 4) return false;
  return parts.every(p => /^\d{1,3}$/.test(p) && Number(p) <= 255);
}

// autorouteAutoadd reads the session's interfaces and routes every attached
// subnet; meterpreter only - a plain shell has no interface table to read.
export function autorouteAutoadd(sid: string): Record<string, string> {
  return { SESSION: sid, CMD: "autoadd" };
}

export function autorouteAdd(sid: string, address: string, prefix: number): Record<string, string> {
  return {
    SESSION: sid, CMD: "add", SUBNET: address,
    NETMASK: prefixToNetmask(prefix) ?? "255.255.255.0",
  };
}

// autorouteRemove deletes through the session that owns the route: the
// module's delete matches subnet, netmask and session together.
export function autorouteRemove(sid: string, target: RouteTarget): Record<string, string> {
  const options: Record<string, string> = {
    SESSION: sid, CMD: "delete", SUBNET: target.subnet,
  };
  if (target.netmask) options.NETMASK = target.netmask;
  return options;
}

// removeRouteItems builds the shared context-menu block for removing routes,
// used by topology route edges (one route) and routed-network cards (many).
export function removeRouteItems(
  routes: readonly { subnet: string; sessionId: string }[],
  onRemove: (route: { subnet: string; sessionId: string }) => void,
): MenuItem[] {
  const live = routes.filter(r => r.subnet && r.sessionId);
  if (live.length === 0) return [];
  const items: MenuItem[] = [{ head: live[0]!.subnet, sub: "pivot route" }];
  for (const route of live) {
    items.push({
      icon: "signpost",
      label: `Remove route via session ${route.sessionId}`,
      danger: true,
      fn: () => onRemove(route),
    });
  }
  return items;
}

// runAutoroute fires the module and lets the engine's launch/route events
// narrate the result; throwers (CommandError) are the dialog's to show.
// The ws singleton is imported lazily: it opens a socket at module load,
// which unit tests (no `location`) must not trigger by importing this file.
export async function runAutoroute(options: Record<string, string>): Promise<void> {
  const { ws } = await import("../ws/singleton");
  await ws.command("module.execute", { type: "post", name: AUTOROUTE_MODULE, options });
}
