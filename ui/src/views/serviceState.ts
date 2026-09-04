// serviceOpen mirrors the engine's rule for reachable services: a row
// counts when its state is empty (not every scanner records one) or says
// open. Closed or filtered ports must never be offered as login targets
// or counted among a host's open services.
export function serviceOpen(s: { state?: string }): boolean {
  return !s.state || s.state === "open";
}
