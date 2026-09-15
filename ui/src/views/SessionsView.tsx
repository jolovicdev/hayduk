import { Show, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { openContextMenu, openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
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

  function menuItems(row: SessionState): Parameters<typeof openContextMenuFor>[1] {
    const items: Parameters<typeof openContextMenuFor>[1] = [
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
      { icon: "copy", label: "Copy user", fn: () => copyWithFeedback(row.username ?? "") },
    );
    return items;
  }

  function menu(row: SessionState, e: MouseEvent) {
    openContextMenuFor(e, menuItems(row));
  }

  // The button provides menu access for touch and keyboard input.
  function rowMenuButton(row: SessionState, e: MouseEvent) {
    // Block both the row's Interact handler and the document listener
    // that would close the menu on this click.
    e.stopImmediatePropagation();
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openContextMenu(rect.left, rect.bottom + 4, menuItems(row));
  }

  function kill(sid: string) {
    ws.command("session.stop", { sid })
      .catch((e: any) => flash(e?.message ?? `could not stop session ${sid}`));
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
        emptyTitle="No sessions"
        emptyIcon="broadcast"
        empty="Sessions appear here when an exploit or handler opens a connection."
        onRowClick={(r) => props.onInteract(r.id)}
        onRowContextMenu={menu}
        columns={[
          { key: "id", label: "ID", mono: true, width: "48px" },
          { key: "type", label: "TYPE", render: (r) => r.info
            ? <><b>{r.type}</b> <span class="row-info" title={r.info}>{r.info}</span></>
            : <b>{r.type}</b> },
          { key: "username", label: "USER" },
          { key: "targetHost", label: "HOST", mono: true },
          { key: "viaExploit", label: "VIA", mono: true, render: (r) => <span class="dim">{r.viaExploit}</span> },
          { key: "openedAt", label: "OPENED", mono: true, render: (r) => ageOf(r.openedAt, now()) },
          {
            key: "actions", label: "", width: "44px",
            render: (r) => (
              <button class="rowmenu" aria-label={`Actions for session ${r.id}`} title="Session actions"
                onClick={(e) => rowMenuButton(r, e)}>
                <i aria-hidden="true" class="ph ph-dots-three"></i>
              </button>
            ),
          },
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
