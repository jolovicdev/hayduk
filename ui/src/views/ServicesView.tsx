import { DataTable } from "../components/DataTable";
import { campaignState } from "../stores/store";
import { openContextMenuFor } from "../components/contextmenu";
import { copyWithFeedback } from "../clipboard";
import type { ServiceState } from "../protocol/types";

export function ServicesView(props: { onInspect: (addr: string) => void; selected?: string }) {
  const rows = () => campaignState().services.filter((s): s is ServiceState => !!s);

  return (
    <div class="svcwrap">
      <DataTable
        rows={rows()}
        rowKey={(r) => r.host}
        selectedKey={() => props.selected}
        emptyTitle="No services"
        emptyIcon="stack"
        empty="No services discovered yet; run a services scan or open a host in the inspector."
        onRowClick={(r) => props.onInspect(r.host)}
        onRowContextMenu={(r, e) => {
          openContextMenuFor(e, [
            { head: `${r.host}:${r.port}`, sub: `${r.proto} ${r.name}` },
            { icon: "info", label: "Inspect host", fn: () => props.onInspect(r.host) },
            { icon: "copy", label: "Copy address", hint: r.host, fn: () => copyWithFeedback(r.host) },
          ]);
        }}
        columns={[
          { key: "host", label: "HOST", render: (r) => <b>{r.host}</b> },
          { key: "port", label: "PORT", mono: true },
          { key: "proto", label: "PROTO", mono: true },
          { key: "name", label: "SERVICE" },
          { key: "state", label: "STATE", render: (r) => <span class="st">{r.state}</span> },
          { key: "info", label: "INFO", render: (r) => <span class="dim">{r.info}</span> },
        ]}
      />
    </div>
  );
}
