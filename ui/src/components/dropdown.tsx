import { JSX, Show, createSignal, onCleanup, onMount } from "solid-js";

export function Dropdown(props: { id: string; label: string; children: JSX.Element }) {
  const [open, setOpen] = createSignal(false);
  let wrap: HTMLDivElement | undefined;

  onMount(() => {
    const onDoc = (e: MouseEvent) => {
      if (open() && wrap && !wrap.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("click", onDoc);
    document.addEventListener("keydown", onKey);
    onCleanup(() => {
      document.removeEventListener("click", onDoc);
      document.removeEventListener("keydown", onKey);
    });
  });

  return (
    <div class="menuwrap" ref={wrap}>
      <button class="mbtn" classList={{ open: open() }} aria-haspopup="true"
        onClick={(e) => { e.stopPropagation(); setOpen(!open()); }}>
        {props.label}
      </button>
      <Show when={open()}>
        <div class="dropdown show" role="menu" onClick={() => setOpen(false)}>{props.children}</div>
      </Show>
    </div>
  );
}

export function MenuItemButton(props: {
  icon?: string;
  label: string;
  hint?: string;
  disabled?: boolean;
  danger?: boolean;
  onClick?: () => void;
}) {
  return (
    <button class={`ditem${props.danger ? " danger" : ""}`} disabled={props.disabled} onClick={() => props.onClick?.()}>
      <Show when={props.icon}><i class={`ph ph-${props.icon}`}></i></Show>
      <span>{props.label}</span>
      <Show when={props.hint}><span class="kbd">{props.hint}</span></Show>
    </button>
  );
}
