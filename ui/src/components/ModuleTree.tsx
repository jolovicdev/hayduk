import { For, Show, createMemo, createSignal } from "solid-js";
import { buildModuleTree, filterTree, visibleChildren, type TreeNode } from "./tree";
import { rankChip } from "./rank";
import { campaignState } from "../stores/store";
import { openContextMenuFor } from "./contextmenu";

export function ModuleTree(props: { onLaunch: (type: string, path: string) => void }) {
  const [query, setQuery] = createSignal("");
  const [open, setOpen] = createSignal(new Set<string>(["exploit"]));

  const root = createMemo(() => {
    const m = campaignState().modules as unknown as Record<string, string[]> | null | undefined;
    if (!m) return null;
    return buildModuleTree(m as Record<string, string[]>);
  });
  const keep = createMemo(() => (root() ? filterTree(root()!, query()) : new Set<string>()));
  const total = createMemo(() => {
    const m = campaignState().modules;
    if (!m) return 0;
    return [m.exploits, m.auxiliary, m.post, m.payloads, m.encoders, m.nops, m.evasion]
      .reduce((a, v) => a + (v?.length ?? 0), 0);
  });

  function toggle(path: string) {
    setOpen(prev => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function nodeMenu(e: MouseEvent, node: TreeNode, type: string) {
    const items: Parameters<typeof openContextMenuFor>[1] = [
      { head: node.name, sub: node.path },
    ];
    // only a leaf is a module; launching a category branch would dispatch
    // its folder path as a refname and msf would reject it. Type roots of
    // empty collections are childless too, so they are excluded by path:
    // a type root's path is the bare type, a leaf's never is.
    if (node.children.length === 0 && node.path !== type) {
      items.push({ icon: "rocket-launch", label: "Launch…", fn: () => props.onLaunch(type, node.path) });
    }
    items.push(
      { sep: true },
      { icon: "copy", label: "Copy path", fn: () => navigator.clipboard.writeText(node.path) },
    );
    openContextMenuFor(e, items);
  }

  return (
    <Show when={root()} fallback={
      <nav class="tree"><div class="noresult">{total() === 0 ? "Not connected." : "No modules."}</div></nav>
    }>
      {(r) => (
        <nav class="tree" classList={{ filtered: !!query() && keep().size === 0 }}>
          <div class="filterbox">
            <i class="ph ph-magnifying-glass"></i>
            <input placeholder="Filter modules" value={query()}
              onInput={(e) => setQuery(e.currentTarget.value)} autocomplete="off" spellcheck={false}
              aria-label="Filter modules" />
          </div>
          <ul>
            <For each={r().children}>{(typeNode) =>
              <Branch node={typeNode} type={typeNode.type} open={open} toggle={toggle}
                keep={keep()} query={query()} onMenu={nodeMenu} onLaunch={props.onLaunch} />
            }</For>
          </ul>
          <div class="noresult">No module matches that filter.</div>
        </nav>
      )}
    </Show>
  );
}

function Branch(props: {
  node: TreeNode;
  type: string;
  open: () => Set<string>;
  toggle: (p: string) => void;
  keep: Set<string>;
  query: string;
  onMenu: (e: MouseEvent, n: TreeNode, t: string) => void;
  onLaunch: (type: string, path: string) => void;
}) {
  const expanded = () =>
    props.query ? props.keep.has(props.node.path) : props.open().has(props.node.path);
  const visible = () => !props.query || props.keep.has(props.node.path);
  return (
    <Show when={visible()}>
      <li>
        <button class="trow branch" classList={{ expanded: expanded() }} aria-expanded={expanded()}
          onClick={() => props.toggle(props.node.path)}
          onContextMenu={(e) => props.onMenu(e, props.node, props.type)}>
          <i class={`ph ${expanded() ? "ph-caret-down" : "ph-caret-right"} caret`}></i>
          <i class={`ph ph-folder${expanded() ? "-open" : ""} fic`}></i>
          <span class="tlabel">{props.node.name}/</span>
          <Show when={props.node.count > 0}>
            <span class="cnt">{props.node.count.toLocaleString()}</span>
          </Show>
        </button>
        <ul classList={{ open: expanded() }}>
          <For each={visibleChildren(props.node.children, expanded())}>{(child) =>
            child.children.length === 0 ? (
              <li>
                <button class="trow leaf" title={child.path}
                  onDblClick={() => props.onLaunch(props.type, child.path)}
                  onContextMenu={(e) => props.onMenu(e, child, props.type)}>
                  <i class="ph ph-file-code mfil"></i>
                  <span class="tlabel">{child.name}</span>
                  <Show when={rankChip(campaignState().moduleRanks?.[child.path])}>
                    {(chip) => <span class={`rankchip${chip().hot ? " hot" : ""}`}>{chip().label}</span>}
                  </Show>
                </button>
              </li>
            ) : (
              <Branch node={child} type={props.type} open={props.open} toggle={props.toggle}
                keep={props.keep} query={props.query} onMenu={props.onMenu} onLaunch={props.onLaunch} />
            )
          }</For>
        </ul>
      </li>
    </Show>
  );
}
