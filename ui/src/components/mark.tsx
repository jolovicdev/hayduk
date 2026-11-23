import { Show } from "solid-js";

// Crisp geometric mark: obsidian tile with crimson architectural H monogram.
export function HaydukMark(props: { size?: number; tile?: boolean; title?: string }) {
  const size = () => props.size ?? 24;
  return (
    <svg width={size()} height={size()} viewBox="0 0 24 24"
      aria-hidden={props.title ? undefined : "true"} role={props.title ? "img" : undefined}
      classList={{ mark: !props.tile }}>
      <Show when={props.title}>
        <title>{props.title}</title>
      </Show>
      <rect x="1.5" y="1.5" width="21" height="21" rx="4" style="fill:var(--brand)" />
      <g style="fill:#090a0c">
        <rect x="6.8" y="6.2" width="2.8" height="11.6" />
        <rect x="14.4" y="6.2" width="2.8" height="11.6" />
        <rect x="6.8" y="10.7" width="10.4" height="2.6" />
      </g>
    </svg>
  );
}
