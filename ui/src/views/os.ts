export function osBadge(h: { osName?: string }): {
  key: "win" | "linux" | "other";
  label: string;
} {
  const n = (h.osName || "").toLowerCase();
  if (n.includes("windows")) {
    const label = (h.osName || "windows")
      .replace("Microsoft Windows", "WIN")
      .replace("Windows Server", "SRV")
      .replace("Windows", "WIN")
      .toUpperCase();
    return { key: "win", label };
  }
  const linuxHints = ["linux", "ubuntu", "debian", "centos", "red hat", "redhat", "fedora", "suse"];
  if (linuxHints.some(k => n.includes(k))) {
    return { key: "linux", label: (h.osName || "linux").toUpperCase().slice(0, 14) };
  }
  return { key: "other", label: (h.osName || "unknown").toUpperCase().slice(0, 14) };
}
