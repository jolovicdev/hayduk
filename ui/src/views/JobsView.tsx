import { DataTable } from "../components/DataTable";
import { openContextMenuFor } from "../components/contextmenu";
import { copyWithFeedback } from "../clipboard";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ageOf } from "./format";
import { jobRows } from "./jobs";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";

export function stopJob(id: string) {
  ws.command("job.stop", { id })
    .catch((e: any) => flash(e?.message ?? `could not stop job ${id}`));
}

export function JobsView(props: { onOpenModule?: (module: string) => void }) {
  // job ages must tick even while the jobs map itself is quiet
  const now = createNowSignal(10_000);
  const rows = () => jobRows(campaignState().jobs);

  return (
    <DataTable
      rows={rows()}
      rowKey={(r) => r.id}
      emptyTitle="No jobs"
      emptyIcon="gear"
      empty="No jobs running. Exploit handlers and long-running modules appear here the moment msf starts them."
      onRowContextMenu={(r, e) => {
        openContextMenuFor(e, [
          { head: `Job ${r.id}`, sub: r.module },
          { icon: "copy", label: "Copy module path", fn: () => copyWithFeedback(r.module) },
          { sep: true },
          { icon: "terminal-window", label: "Open module in console", hint: "use",
            fn: () => props.onOpenModule?.(r.module) },
          { sep: true },
          { icon: "x", label: "Stop job", danger: true, fn: () => stopJob(r.id) },
        ]);
      }}
      columns={[
        { key: "id", label: "ID", mono: true, width: "56px" },
        { key: "kind", label: "Kind", width: "90px", render: (r) => <span class="dim">{r.kind || "-"}</span> },
        { key: "module", label: "Module", mono: true },
        { key: "startedAt", label: "Running", mono: true, width: "90px", render: (r) => ageOf(r.startedAt, now()) },
        { key: "actions", label: "", width: "70px", render: (r) => (
          <button class="abtn" onClick={() => stopJob(r.id)}>Stop</button>
        ) },
      ]}
    />
  );
}
