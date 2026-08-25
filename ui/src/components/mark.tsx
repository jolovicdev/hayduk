import { Show } from "solid-js";

// The hayduk mark: a white H on the oxide-red box, drawn as three rects
// so it stays crisp at every size.
export function HaydukMark(props: { size?: number; tile?: boolean; title?: string }) {
  const size = () => props.size ?? 24;
  return (
    <svg width={size()} height={size()} viewBox="0 0 24 24"
      aria-hidden={props.title ? undefined : "true"} role={props.title ? "img" : undefined}
      classList={{ mark: !props.tile }}>
      <Show when={props.title}>
        <title>{props.title}</title>
      </Show>
      <rect x="2" y="2" width="20" height="20" rx="5" fill="#c9404a" />
      <g fill="#f4f6f9">
        <rect x="7.2" y="6.8" width="2.6" height="10.4" />
        <rect x="14.2" y="6.8" width="2.6" height="10.4" />
        <rect x="7.2" y="10.8" width="9.6" height="2.4" />
      </g>
    </svg>
  );
}
