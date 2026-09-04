import { DataTable } from "../components/DataTable";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import type { CredState } from "../protocol/types";

export function CredsView() {
  const rows = () => campaignState().creds.filter((c): c is CredState => !!c);

  return (
    <DataTable
      rows={rows()}
      rowKey={(r) => `${r.host}:${r.port}:${r.user}:${r.pass}`}
      onRowContextMenu={(r, e) => {
        openContextMenuFor(e, [
          { head: r.user || "(no user)", sub: `recovered credential on ${r.host}` },
          { sep: true },
          { icon: "copy", label: "Copy value", fn: () => navigator.clipboard.writeText(r.pass) },
          { icon: "copy", label: "Copy user", fn: () => navigator.clipboard.writeText(r.user) },
        ]);
      }}
      columns={[
        { key: "host", label: "HOST", mono: true },
        { key: "user", label: "USER", render: (r) => <b>{r.user}</b> },
        { key: "type", label: "TYPE", mono: true, render: (r) => <span class="dim">{r.type || "password"}</span> },
        { key: "pass", label: "VALUE", mono: true },
        { key: "service", label: "SERVICE", render: (r) => <span class="dim">{r.service || ""}</span> },
      ]}
    />
  );
}
