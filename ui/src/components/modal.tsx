import { JSX, createUniqueId, onCleanup, onMount } from "solid-js";

export function Modal(props: { title: string; onClose: () => void; children: JSX.Element; width?: string }) {
  let card: HTMLDivElement | undefined;
  const titleId = createUniqueId();
  onMount(() => {
    // dialogs take focus on open and hand it back on close
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    card?.focus();
    onCleanup(() => previous?.focus());
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
