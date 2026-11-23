import { Match, Switch } from "solid-js";

export type OsFamily = "win" | "linux" | "other";

// Shared SVG internals for a computer monitor with an embedded OS emblem.
// Coordinates live in a 42x42 space so TopologyGraph (<g>) and Inspector (<svg>)
// render the exact same crisp geometric hardware icon.
export function OsHardwareGlyph(props: { family: OsFamily; state?: "access" | "login" | "idle" }) {
  return (
    <g class={`hw-tile ${props.family} ${props.state ?? "idle"}`}>
      <rect class="hw-bg" width="42" height="42" rx="6" />
      {/* monitor bezel */}
      <rect class="hw-screen" x="6" y="7" width="30" height="21" rx="2.5" />
      {/* monitor stand */}
      <path class="hw-stand" d="M 16 33.5 H 26 M 21 28 V 33.5" />
      {/* OS emblem inside the screen */}
      <Switch fallback={
        <g class="hw-emblem other">
          <path d="M 13 13.5 H 29 M 13 17.5 H 29 M 13 21.5 H 23" />
        </g>
      }>
        <Match when={props.family === "win"}>
          <g class="hw-emblem win">
            <rect x="14.5" y="11" width="5.8" height="5.8" rx="0.6" />
            <rect x="21.7" y="11" width="5.8" height="5.8" rx="0.6" />
            <rect x="14.5" y="18.2" width="5.8" height="5.8" rx="0.6" />
            <rect x="21.7" y="18.2" width="5.8" height="5.8" rx="0.6" />
          </g>
        </Match>
        <Match when={props.family === "linux"}>
          <g class="hw-emblem linux">
            <path d="M 13.5 13.2 L 18.5 17.5 L 13.5 21.8" />
            <path d="M 21 21.8 H 28" />
          </g>
        </Match>
      </Switch>
      {/* status LED beacon on top-right corner of the hardware tile */}
      {props.state === "access" && (
        <circle class="hw-led access" cx="36.5" cy="5.5" r="4" />
      )}
      {props.state === "login" && (
        <circle class="hw-led login" cx="36.5" cy="5.5" r="4" />
      )}
    </g>
  );
}

export function OsIconBadge(props: { family: OsFamily; state?: "access" | "login" | "idle"; size?: number }) {
  const size = () => props.size ?? 38;
  return (
    <svg class="os-hw-svg" width={size()} height={size()} viewBox="0 0 42 42" aria-hidden="true">
      <OsHardwareGlyph family={props.family} state={props.state} />
    </svg>
  );
}
