import { onCleanup, onMount } from "solid-js";

export type MenuItem = {
  head?: string;
  sub?: string;
  icon?: string;
  label?: string;
  hint?: string;
  danger?: boolean;
  sep?: boolean;
  fn?: () => void;
};

function esc(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export function buildMenuHTML(items: MenuItem[]): string {
  return items
    .map(it => {
      if (it.sep) return '<div class="csep"></div>';
      if (it.head) {
        return (
          `<div class="chead"><div class="chn">${esc(it.head)}</div>` +
          (it.sub ? `<div class="chi">${esc(it.sub)}</div>` : "") +
          "</div>"
        );
      }
      return (
        `<button class="citem${it.danger ? " danger" : ""}">` +
        (it.icon ? `<i class="ph ph-${esc(it.icon)}"></i>` : "<i></i>") +
        `<span>${esc(it.label ?? "")}</span>` +
        (it.hint ? `<span class="hint">${esc(it.hint)}</span>` : "") +
        "</button>"
      );
    })
    .join("");
}

let menuEl: HTMLDivElement | null = null;

export function closeContextMenu() {
  menuEl?.classList.remove("show");
}

export function contextMenuOpen(): boolean {
  return menuEl?.classList.contains("show") ?? false;
}

// placeMenu fits the menu inside the viewport without ever going negative.
export function placeMenu(x: number, y: number, w: number, h: number, vw: number, vh: number) {
  return {
    x: Math.max(0, Math.min(x, vw - w - 8)),
    y: Math.max(0, Math.min(y, vh - h - 8)),
  };
}

// The entry point for contextmenu events. The event must die here: Solid
// delegates contextmenu to document, where the close listener registered
// below also sits, and only the immediate variant stops another listener on
// that same node — plain stopPropagation (or nothing at all) lets that
// listener close the freshly opened menu in the same tick.
export function openContextMenuFor(e: MouseEvent, items: MenuItem[]) {
  e.preventDefault();
  e.stopImmediatePropagation();
  openContextMenu(e.clientX, e.clientY, items);
}

export function openContextMenu(x: number, y: number, items: MenuItem[]) {
  if (!menuEl) return;
  menuEl.innerHTML = buildMenuHTML(items);
  let i = 0;
  const actionable = items.filter(it => it.label !== undefined);
  menuEl.querySelectorAll<HTMLButtonElement>("button.citem").forEach(btn => {
    const it = actionable[i++];
    if (it?.fn) btn.addEventListener("click", () => { closeContextMenu(); it.fn!(); });
  });
  menuEl.classList.add("show");
  const w = menuEl.offsetWidth, h = menuEl.offsetHeight;
  const pos = placeMenu(x, y, w, h, window.innerWidth, window.innerHeight);
  menuEl.style.left = pos.x + "px";
  menuEl.style.top = pos.y + "px";
}

export function ContextMenuRoot() {
  let el!: HTMLDivElement;
  onMount(() => {
    menuEl = el;
    const closeIfOutside = (e: Event) => {
      if (!(e.target as HTMLElement).closest("#ctx")) closeContextMenu();
    };
    document.addEventListener("click", closeIfOutside);
    document.addEventListener("contextmenu", closeIfOutside);
    window.addEventListener("blur", closeContextMenu);
    window.addEventListener("resize", closeContextMenu);
    // the root remounts during HMR and some tests; piled-up listeners would
    // keep closing menus forever
    onCleanup(() => {
      document.removeEventListener("click", closeIfOutside);
      document.removeEventListener("contextmenu", closeIfOutside);
      window.removeEventListener("blur", closeContextMenu);
      window.removeEventListener("resize", closeContextMenu);
    });
  });
  return <div class="ctx" ref={el} id="ctx" role="menu"></div>;
}
