import { createMemo } from "solid-js";
import { DataTable } from "../components/DataTable";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ageOf } from "./format";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
import { operationProgress, operationsRows } from "./operations";

export function cancelOperation(id: string) {
  ws.command("operations.cancel", { id })
    .catch((e: any) => flash(e?.message ?? `could not cancel ${id}`));
}

export function OperationsView(props: { active?: () => boolean }) {
  const now = createNowSignal(10_000, () => props.active?.() ?? true);
  const rows = createMemo(() => operationsRows(campaignState().operations));

  function statusClass(status: string) {
    switch (status) {
      case "running": return "st";
      case "done": return "amb";
      case "failed": return "dim";
      default: return "dim";
    }
  }

  return (
    <DataTable
      rows={rows()}
      rowKey={(r) => r.id}
      emptyTitle="No operations"
      emptyIcon="pulse"
      empty="Volume scans, brute-force runs and Hail Marys started from this campaign appear here, with progress that survives a page reload."
      onRowContextMenu={(r, e) => {
        openContextMenuFor(e, [
          { head: `${r.kind} ${r.id}`, sub: r.target },
          ...(r.status === "running"
            ? [{ icon: "x", label: "Cancel operation", danger: true, fn: () => cancelOperation(r.id) }]
            : []),
          { icon: "copy", label: "Copy summary", fn: () => copyWithFeedback(r.summary ?? r.status) },
        ]);
      }}
      columns={[
        { key: "kind", label: "Kind", width: "96px", render: (r) => <b>{r.kind}</b> },
        { key: "target", label: "Target", render: (r) => <span class="dim">{r.target || "-"}</span> },
        { key: "status", label: "Status", width: "96px", render: (r) => (
          <span class={statusClass(r.status)}>{r.status}</span>
        ) },
        { key: "progress", label: "Progress", mono: true, width: "90px", render: (r) => operationProgress(r) },
        { key: "operator", label: "Operator", width: "110px", render: (r) => r.operator || "-" },
        { key: "summary", label: "Result", render: (r) => <span class="dim">{r.summary || ""}</span> },
        { key: "startedAt", label: "Started", mono: true, width: "80px", render: (r) => ageOf(r.startedAt, now()) },
        { key: "actions", label: "", width: "76px", render: (r) => (
          r.status === "running"
            ? <button class="abtn" onClick={() => cancelOperation(r.id)}>Cancel</button>
            : null
        ) },
      ]}
    />
  );
}
