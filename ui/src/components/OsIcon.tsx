export type OsFamily = "win" | "linux" | "apple" | "android" | "other";

// Phosphor Icons codepoints (kept in sync by scripts/subset-icons.sh).
// Referencing the ph-* class names here ensures subset-icons.sh includes
// every OS glyph in phosphor-regular.woff2 and phosphor-fill.woff2.
const PH_CLASS: Record<OsFamily, string> = {
  win: "ph-fill ph-windows-logo",
  linux: "ph-fill ph-linux-logo",
  apple: "ph-fill ph-apple-logo",
  android: "ph-fill ph-android-logo",
  other: "ph ph-hard-drives",
};

const PH_MONITOR_CLASS = "ph ph-monitor";
const PH_MONITOR_CHAR = "\ue32e";

const PH_OS_CHAR: Record<OsFamily, string> = {
  win: "\ue692",
  linux: "\ueb02",
  apple: "\ue516",
  android: "\ue008",
  other: "\ue2a0",
};

export function detectOsFamily(host: { osName?: string; osFlavor?: string } | undefined): OsFamily {
  const s = `${host?.osName ?? ""} ${host?.osFlavor ?? ""}`.toLowerCase();
  if (s.includes("win")) return "win";
  if (s.includes("android")) return "android";
  if (s.includes("mac") || s.includes("darwin") || s.includes("apple") || s.includes("ios")) return "apple";
  if (
    s.includes("linux") || s.includes("ubuntu") || s.includes("debian") ||
    s.includes("centos") || s.includes("fedora") || s.includes("alpine") ||
    s.includes("rocky") || s.includes("rhel") || s.includes("kali") ||
    s.includes("arch") || s.includes("bsd") || s.includes("unix")
  ) return "linux";
  return "other";
}

// Shared SVG internals for a computer monitor (Phosphor ph-monitor) with an
// official Phosphor OS logo centered inside the screen. Coordinates live in a
// 42x42 viewBox so TopologyGraph (<g>) and Inspector (<svg>) render identically.
export function OsHardwareGlyph(props: { family: OsFamily; state?: "access" | "login" | "idle" }) {
  const glyphClass = () => (props.family === "other" ? "hw-os-glyph ph" : "hw-os-glyph ph-fill");
  return (
    <g
      class={`hw-tile ${props.family} ${props.state ?? "idle"}`}
      data-monitor-icon={PH_MONITOR_CLASS}
      data-os-icon={PH_CLASS[props.family]}
    >
      <rect class="hw-bg" width="42" height="42" rx="7" />
      <text
        class={`hw-monitor ${PH_MONITOR_CLASS}`}
        x="21"
        y="22"
        text-anchor="middle"
        dominant-baseline="central"
      >
        {PH_MONITOR_CHAR}
      </text>
      <text
        class={`${glyphClass()} ${props.family}`}
        x="21"
        y="19.3"
        text-anchor="middle"
        dominant-baseline="central"
      >
        {PH_OS_CHAR[props.family]}
      </text>
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
