import { Show, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { Modal } from "../components/modal";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
import type { LootState } from "../protocol/types";

export function LootView() {
  const rows = () => campaignState().loot.filter((l): l is LootState => !!l);
  const [viewing, setViewing] = createSignal<{ row: LootState; data: string } | null>(null);

  async function view(row: LootState) {
    try {
      const res = await ws.command<{ data: string }>("loot.download", {
        host: row.host, type: row.type, name: row.name,
      });
      setViewing({ row, data: res.data });
    } catch (e: any) {
      flash(e?.message ?? "loot lookup failed");
    }
  }

  function downloadFile(name: string, data: string) {
    const blob = new Blob([data], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = name || "loot.txt";
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <>
    <DataTable
      rows={rows()}
      rowKey={(r) => `${r.host}:${r.type}:${r.name}`}
      emptyTitle="No loot"
      emptyIcon="archive"
      empty="Captured files and tokens appear here."
      onRowContextMenu={(r, e) => {
        openContextMenuFor(e, [
          { head: r.name, sub: `${r.type} on ${r.host}` },
          { icon: "download-simple", label: "View stored data", fn: () => void view(r) },
          { icon: "copy", label: "Copy name", fn: () => copyWithFeedback(r.name) },
        ]);
      }}
      columns={[
        { key: "host", label: "Host", mono: true },
        { key: "type", label: "Type", mono: true },
        { key: "name", label: "Name" },
        { key: "info", label: "Info", render: (r) => <span class="dim">{r.info}</span> },
        { key: "actions", label: "", width: "90px", render: (r) => (
          <button class="abtn" onClick={() => void view(r)}>View data</button>
        ) },
      ]}
    />
    <Show when={viewing()}>
      {(v) => (
        <Modal title={v().row.name} onClose={() => setViewing(null)} width="560px">
          <p>{v().row.type} on {v().row.host}. Stored value from the msf database:</p>
          <pre class="toolout" style="margin-top:12px">{v().data || "(empty)"}</pre>
          <div class="mbtns">
            <button class="abtn" onClick={() => copyWithFeedback(v().data)}>Copy</button>
            <button class="abtn" onClick={() => downloadFile(v().row.name, v().data)}>Download</button>
          </div>
        </Modal>
      )}
    </Show>
    </>
  );
}
