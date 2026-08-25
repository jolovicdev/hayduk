export type TreeNode = { name: string; path: string; type: string; count: number; children: TreeNode[] };

function node(name: string, path: string, type: string): TreeNode {
  return { name, path, type, count: 0, children: [] };
}

// The wire ModuleIndex keys collections by plural; msf module types are the
// singular form and every rpc call wants the singular.
const COLLECTION_TYPES: Record<string, string> = {
  exploits: "exploit",
  auxiliary: "auxiliary",
  post: "post",
  payloads: "payload",
  encoders: "encoder",
  nops: "nop",
  evasion: "evasion",
};

// buildModuleTree nests a module index for browsing. Type roots keep the
// plural collection key as their display name and carry the msf module type
// in .type; leaf paths are bare module refnames - the exact string
// module.execute, the rank cache and the msf console expect.
export function buildModuleTree(index: Record<string, string[] | undefined>): TreeNode {
  const root = node("", "", "");
  for (const [collection, modules] of Object.entries(index)) {
    const type = COLLECTION_TYPES[collection];
    if (!type) continue;
    const typeNode = node(collection, type, type);
    for (const path of modules ?? []) {
      const parts = path.split("/");
      let cur = typeNode;
      cur.count++;
      for (let i = 0; i < parts.length; i++) {
        if (i === parts.length - 1) {
          cur.children.push(node(parts[i]!, path, type));
        } else {
          let next = cur.children.find(c => c.name === parts[i] && c.children.length > 0);
          if (!next) {
            next = node(parts[i]!, cur.path === "" ? parts[i]! : `${cur.path}/${parts[i]}`, type);
            cur.children.push(next);
          }
          next.count++;
          cur = next;
        }
      }
    }
    sortRecursive(typeNode);
    root.children.push(typeNode);
  }
  root.children.sort((a, b) => a.name.localeCompare(b.name));
  return root;
}

function sortRecursive(n: TreeNode) {
  n.children.sort((a, b) => a.name.localeCompare(b.name));
  for (const c of n.children) sortRecursive(c);
}

// filterTree returns the set of node paths that match the query or contain a
// matching descendant; empty query returns an empty set (show everything).
export function filterTree(root: TreeNode, query: string): Set<string> {
  const keep = new Set<string>();
  if (!query) return keep;
  const q = query.toLowerCase();
  function walk(n: TreeNode): boolean {
    let childHit = false;
    for (const c of n.children) childHit = walk(c) || childHit;
    const selfHit = n.path.toLowerCase().includes(q);
    if (childHit || selfHit) keep.add(n.path);
    return childHit || selfHit;
  }
  walk(root);
  return keep;
}
