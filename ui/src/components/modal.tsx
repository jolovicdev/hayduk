import { JSX, createUniqueId, onCleanup, onMount } from "solid-js";
import { contextMenuOpen } from "./contextmenu";
import { escapeCloser } from "./escape";

export function Modal(props: { title: string; onClose: () => void; children: JSX.Element; width?: string }) {
  let card: HTMLDivElement | undefined;
  const titleId = createUniqueId();
  onMount(() => {
    // dialogs take focus on open and hand it back on close
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    card?.focus();
    onCleanup(() => previous?.focus());
    const onKey = escapeCloser({ onClose: props.onClose, overlayOpen: contextMenuOpen });
    document.addEventListener("keydown", onKey);
    onCleanup(() => document.removeEventListener("keydown", onKey));
    // Tab cycles inside the dialog: the page behind an aria-modal overlay
    // must not receive focus
    const trapTab = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || !card) return;
      const focusables = [...card.querySelectorAll<HTMLElement>(
        'button, input, select, textarea, a[href], [tabindex]:not([tabindex="-1"])',
      )].filter(el => !el.hasAttribute("disabled"));
      if (focusables.length === 0) return;
      const first = focusables[0]!;
      const last = focusables[focusables.length - 1]!;
      const active = document.activeElement;
      if (e.shiftKey && (active === first || active === card || !card.contains(active))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && (active === last || active === card || !card.contains(active))) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", trapTab, true);
    onCleanup(() => document.removeEventListener("keydown", trapTab, true));
  });
  return (
    <div class="modalback show" onClick={(e) => { if (e.target === e.currentTarget) props.onClose(); }}>
      <div class="modal" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={card} tabIndex={-1}
        style={props.width ? `width:${props.width}` : ""}>
        <div class="mhead">
          <div>
            <div class="mtitle" id={titleId}>{props.title}</div>
          </div>
          <button class="zbtn" style="margin-left:auto" onClick={props.onClose} aria-label="Close">
            <i class="ph ph-x"></i>
          </button>
        </div>
        {props.children}
      </div>
    </div>
  );
}
