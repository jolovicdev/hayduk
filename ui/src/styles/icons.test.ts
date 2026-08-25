import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const srcRoot = join(here, "..");

function phosphorNames(weight: "regular" | "fill"): Set<string> {
  const css = readFileSync(
    join(here, "..", "..", "node_modules", "@phosphor-icons", "web", "src", weight, "style.css"),
    "utf8",
  );
  const prefix = weight === "fill" ? "ph-fill" : "ph";
  return new Set([...css.matchAll(new RegExp(`\\.${prefix}\\.ph-([a-z0-9-]+):before`, "g"))].map(m => m[1]!));
}

// every icon name the source can render: ph- class usages plus any quoted
// string that is a real phosphor name (covers names built from data at
// runtime - menu items, tab tuples, ternaries)
function referencedNames(): Set<string> {
  const names = new Set<string>();
  const walk = (dir: string) => {
    for (const ent of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, ent.name);
      if (ent.isDirectory()) { walk(p); continue; }
      if (!/\.(tsx?|css)$/.test(ent.name)) continue;
      const text = readFileSync(p, "utf8");
      for (const m of text.matchAll(/\bph-([a-z0-9-]+)/g)) names.add(m[1]!);
      for (const m of text.matchAll(/(["'])([a-z0-9][a-z0-9-]*)\1/g)) names.add(m[2]!);
    }
  };
  walk(srcRoot);
  for (const w of ["fill", "bold", "thin", "light", "duotone"]) names.delete(w);
  names.add("folder-open"); // template suffix, not a literal
  return names;
}

function shippedNames(): Set<string> {
  const css = readFileSync(join(here, "icons.css"), "utf8");
  return new Set([...css.matchAll(/\.ph(?:-fill)?\.ph-([a-z0-9-]+):before/g)].map(m => m[1]!));
}

describe("icon subset", () => {
  it("ships every icon the source can render", () => {
    const known = phosphorNames("regular");
    const shipped = shippedNames();
    const missing = [...referencedNames()].filter(n => known.has(n) && !shipped.has(n));
    expect(missing).toEqual([]);
  });
});
