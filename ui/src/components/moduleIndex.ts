import type { ModuleIndex } from "../protocol/types";

export interface IndexedModule {
  type: string;
  path: string;
  haystack: string;
  leaf: string;
}

const BUCKETS: readonly { prop: keyof ModuleIndex; type: string }[] = [
  { prop: "exploits", type: "exploit" },
  { prop: "auxiliary", type: "auxiliary" },
  { prop: "post", type: "post" },
  { prop: "payloads", type: "payload" },
  { prop: "encoders", type: "encoder" },
  { prop: "evasion", type: "evasion" },
  { prop: "nops", type: "nop" },
];

// Flat pre-lowercased index built once per catalogue load so searching across
// 7,000+ modules in the command palette does zero per-keystroke allocations.
export function buildModuleIndex(catalog: ModuleIndex | null | undefined): IndexedModule[] {
  if (!catalog) return [];
  const out: IndexedModule[] = [];
  for (const { prop, type } of BUCKETS) {
    const list = catalog[prop];
    if (!list) continue;
    for (const path of list) {
      const lower = path.toLowerCase();
      const slash = lower.lastIndexOf("/");
      out.push({
        type,
        path,
        haystack: `${type}/${lower}`,
        leaf: slash >= 0 ? lower.slice(slash + 1) : lower,
      });
    }
  }
  return out;
}

export function searchModuleIndex(
  index: readonly IndexedModule[],
  terms: readonly string[],
  limit = 24,
): IndexedModule[] {
  if (terms.length === 0 || index.length === 0) return [];
  const primary: IndexedModule[] = [];
  const secondary: IndexedModule[] = [];
  const first = terms[0]!;
  for (let i = 0; i < index.length; i++) {
    const item = index[i]!;
    let ok = true;
    for (let j = 0; j < terms.length; j++) {
      if (!item.haystack.includes(terms[j]!)) {
        ok = false;
        break;
      }
    }
    if (!ok) continue;
    if (item.leaf.includes(first)) {
      primary.push(item);
      if (primary.length >= limit) break;
    } else if (secondary.length < limit) {
      secondary.push(item);
    }
  }
  if (primary.length >= limit) return primary;
  return [...primary, ...secondary.slice(0, limit - primary.length)];
}
