import type { Position } from "./layout";

const STORAGE_KEY = "hayduk.topology.positions.v3";

export function loadPositions(storage: Pick<Storage, "getItem"> = localStorage): Map<string, Position> {
  try {
    const raw = storage.getItem(STORAGE_KEY);
    if (!raw) return new Map();
    const stored = JSON.parse(raw);
    if (!stored || typeof stored !== "object" || Array.isArray(stored)) return new Map();

    const positions = new Map<string, Position>();
    for (const [address, position] of Object.entries(stored)) {
      const p = position as Partial<Position> | null;
      if (
        typeof p?.x === "number" && Number.isFinite(p.x) &&
        typeof p.y === "number" && Number.isFinite(p.y)
      ) {
        positions.set(address, { x: p.x, y: p.y });
      }
    }
    return positions;
  } catch {
    return new Map();
  }
}

export function savePositions(
  positions: Map<string, Position>,
  storage: Pick<Storage, "setItem"> = localStorage,
) {
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(Object.fromEntries(positions)));
  } catch {
    // private mode or exhausted quota: layouts just stop persisting
  }
}

// pruneStale keeps only hosts still in the workspace so the store does not
// grow forever across campaigns.
export function pruneStale(
  positions: Map<string, Position>,
  live: Set<string>,
): Map<string, Position> {
  const pruned = new Map<string, Position>();
  for (const [address, position] of positions) {
    if (live.has(address)) pruned.set(address, position);
  }
  return pruned;
}
