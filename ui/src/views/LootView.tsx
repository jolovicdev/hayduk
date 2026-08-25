import { DataTable } from "../components/DataTable";
import { campaignState } from "../stores/store";
import type { LootState } from "../protocol/types";

export function LootView() {
  const rows = () => campaignState().loot.filter((l): l is LootState => !!l);

  return (
    <DataTable
      rows={rows()}
      rowKey={(r) => `${r.host}:${r.type}:${r.name}`}
      columns={[
        { key: "host", label: "HOST", mono: true },
        { key: "type", label: "TYPE", mono: true },
        { key: "name", label: "NAME" },
        { key: "info", label: "INFO", render: (r) => <span class="dim">{r.info}</span> },
      ]}
    />
  );
}
