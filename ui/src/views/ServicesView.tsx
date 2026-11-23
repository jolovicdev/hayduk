import { createMemo } from "solid-js";
import { DataTable } from "../components/DataTable";
import { campaignState } from "../stores/store";
import { openContextMenuFor } from "../components/contextmenu";
import { copyWithFeedback } from "../clipboard";
import type { ServiceState } from "../protocol/types";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";

export function ServicesView(props: { onInspect: (addr: string) => void; selected?: string }) {
  const services = createMemo(() => campaignState().services);
  // Stable, host-major order keeps a refresh from shuffling rows, and the
  // memo means unrelated campaign updates do not re-filter the table.
  const rows = createMemo(() => services()
    .filter((s): s is ServiceState => !!s)
    .sort((a, b) =>
      a.host.localeCompare(b.host, undefined, { numeric: true })
      || a.port - b.port
      || a.proto.localeCompare(b.proto)));

  return (
    <div class="svcwrap">
      <DataTable
        rows={rows()}
        rowKey={(r) => `${r.host}:${r.proto}:${r.port}`}
        selectedRow={(r) => r.host === props.selected}
        emptyTitle="No services"
        emptyIcon="stack"
        empty="No services discovered yet; run a services scan or open a host in the inspector."
        onRowClick={(r) => props.onInspect(r.host)}
        onRowContextMenu={(r, e) => {
          openContextMenuFor(e, [
            { head: `${r.host}:${r.port}`, sub: `${r.proto} ${r.name}` },
            { icon: "info", label: "Inspect host", fn: () => props.onInspect(r.host) },
            { icon: "copy", label: "Copy address", hint: r.host, fn: () => copyWithFeedback(r.host) },
            { sep: true },
            { icon: "x", label: "Delete service", danger: true, fn: () => {
              ws.command("db.service_delete", { host: r.host, port: r.port, proto: r.proto })
                .catch((e: any) => flash(e?.message ?? "service delete failed"));
            } },
          ]);
        }}
        columns={[
          { key: "host", label: "Host", render: (r) => <b>{r.host}</b> },
          { key: "port", label: "Port", mono: true },
          { key: "proto", label: "Proto", mono: true },
          { key: "name", label: "Service" },
          { key: "state", label: "State", render: (r) => <span class="st">{r.state}</span> },
          { key: "info", label: "Info", render: (r) => <span class="dim">{r.info}</span> },
        ]}
      />
    </div>
  );
}
