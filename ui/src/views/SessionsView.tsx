import { Show, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { openContextMenu } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ws } from "../ws/singleton";
import { PivotDialog } from "./PivotDialog";
import { UpgradeDialog } from "./UpgradeDialog";
import { ageOf } from "./format";
import type { SessionState } from "../protocol/types";

export function SessionsView(props: { onInteract: (sid: string) => void }) {
  const [upgrading, setUpgrading] = createSignal<string | undefined>(undefined);
  const [pivoting, setPivoting] = createSignal<string | undefined>(undefined);
  // ages are rendered from wall-clock time; without a tick they freeze at
  // whatever moment the sessions map last changed
  const now = createNowSignal();
  const rows = () =>
    Object.values(campaignState().sessions)
      .filter((s): s is SessionState => !!s)
      .sort((a, b) => Number(a.id) - Number(b.id));

  function menu(row: SessionState, e: MouseEvent) {
    e.preventDefault();
    const items: Parameters<typeof openContextMenu>[2] = [
      { head: `Session ${row.id}`, sub: row.username || row.info || row.targetHost },
      { icon: "terminal-window", label: "Interact", fn: () => props.onInteract(row.id) },
    ];
    if (row.type === "shell") {
      items.push({ icon: "arrows-clockwise", label: "Upgrade to meterpreter…", fn: () => setUpgrading(row.id) });
    }
    items.push(
      { icon: "signpost", label: "Pivot network…", fn: () => setPivoting(row.id) },
      { icon: "x", label: "Kill session", danger: true, fn: () => void kill(row.id) },
      { sep: true },
      { icon: "copy", label: "Copy user", fn: () => navigator.clipboard.writeText(row.username ?? "") },
    );
    openContextMenu(e.clientX, e.clientY, items);
  }

  function kill(sid: string) {
    void ws.command("session.stop", { sid });
  }

  function pivotType(sid: string | undefined) {
    if (!sid) return false;
    return campaignState().sessions[sid]?.type === "meterpreter";
  }

  return (
    <>
      <DataTable
        rows={rows()}
        rowKey={(r) => r.id}
        onRowClick={(r) => props.onInteract(r.id)}
        onRowContextMenu={menu}
        columns={[
          { key: "id", label: "ID", mono: true, width: "48px" },
          { key: "type", label: "TYPE", render: (r) => <><b>{r.type}</b>{r.info ? ` ${r.info}` : ""}</> },
          { key: "username", label: "USER" },
          { key: "targetHost", label: "HOST", mono: true },
          { key: "viaExploit", label: "VIA", mono: true, render: (r) => <span class="dim">{r.viaExploit}</span> },
          { key: "openedAt", label: "OPENED", mono: true, render: (r) => ageOf(r.openedAt, now()) },
        ]}
      />
      <Show when={upgrading()}>
        {(sid) => <UpgradeDialog sid={sid()} onClose={() => setUpgrading(undefined)} />}
      </Show>
      <Show when={pivoting()}>
        {(sid) => (
          <PivotDialog sid={sid()} meterpreter={pivotType(sid())}
            onClose={() => setPivoting(undefined)} />
        )}
      </Show>
    </>
  );
}
