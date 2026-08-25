import { DataTable } from "../components/DataTable";
import { openContextMenu } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ageOf } from "./format";
import { jobRows } from "./jobs";

export function JobsView() {
  // job ages must tick even while the jobs map itself is quiet
  const now = createNowSignal(10_000);
  const rows = () => jobRows(campaignState().jobs);

  return (
    <DataTable
      rows={rows()}
      rowKey={(r) => r.id}
      empty="No jobs running. Exploit handlers and long-running modules appear here the moment msf starts them."
      onRowContextMenu={(r, e) => {
        e.preventDefault();
        openContextMenu(e.clientX, e.clientY, [
          { head: `Job ${r.id}`, sub: r.module },
          { icon: "copy", label: "Copy module path", fn: () => navigator.clipboard.writeText(r.module) },
          { sep: true },
          { icon: "terminal-window", label: "Open module in console", hint: "use", fn: () =>
            navigator.clipboard.writeText(`use ${r.module}`) },
        ]);
      }}
      columns={[
        { key: "id", label: "ID", mono: true, width: "56px" },
        { key: "kind", label: "KIND", width: "90px", render: (r) => <span class="dim">{r.kind || "-"}</span> },
        { key: "module", label: "MODULE", mono: true },
        { key: "startedAt", label: "RUNNING", mono: true, width: "90px", render: (r) => ageOf(r.startedAt, now()) },
      ]}
    />
  );
}
