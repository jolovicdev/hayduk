import { DataTable } from "../components/DataTable";
import { campaignState } from "../stores/store";
import { openContextMenu } from "../components/contextmenu";
import type { ServiceState } from "../protocol/types";

export function ServicesView(props: { onInspect: (addr: string) => void }) {
  const rows = () => campaignState().services.filter((s): s is ServiceState => !!s);

  return (
    <div class="svcwrap">
      <DataTable
        rows={rows()}
        rowKey={(r) => `${r.host}:${r.port}:${r.proto}`}
        onRowClick={(r) => props.onInspect(r.host)}
        onRowContextMenu={(r, e) => {
          e.preventDefault();
          openContextMenu(e.clientX, e.clientY, [
            { head: `${r.host}:${r.port}`, sub: `${r.proto} ${r.name}` },
            { icon: "info", label: "Inspect host", fn: () => props.onInspect(r.host) },
            { icon: "copy", label: "Copy address", hint: r.host, fn: () => navigator.clipboard.writeText(r.host) },
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
