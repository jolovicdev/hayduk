import { Show } from "solid-js";

// Tactile hardware monogram: signal orange tile with bold carbon H.
export function HaydukMark(props: { size?: number; tile?: boolean; title?: string }) {
  const size = () => props.size ?? 24;
  return (
    <svg width={size()} height={size()} viewBox="0 0 24 24"
      aria-hidden={props.title ? undefined : "true"} role={props.title ? "img" : undefined}
      classList={{ mark: !props.tile }}>
      <Show when={props.title}>
        <title>{props.title}</title>
      </Show>
      <rect x="1.5" y="1.5" width="21" height="21" rx="5" style="fill:var(--brand)" />
      <g style="fill:#120b07">
        <rect x="6.5" y="6" width="3" height="12" rx="0.6" />
        <rect x="14.5" y="6" width="3" height="12" rx="0.6" />
        <rect x="6.5" y="10.6" width="11" height="2.8" rx="0.5" />
      </g>
    </svg>
  );
}
