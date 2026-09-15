import { JSX, For, Show } from "solid-js";
import { EmptyState } from "./EmptyState";

export type Column<T> = {
  key: string;
  label: string;
  mono?: boolean;
  width?: string;
  render?: (row: T) => JSX.Element;
};

export function DataTable<T>(props: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  selectedKey?: () => string | undefined;
  emptyTitle: string;
  empty?: string;
  emptyIcon?: string;
  onRowClick?: (row: T) => void;
  onRowContextMenu?: (row: T, e: MouseEvent) => void;
}) {
  return (
    <div class="dtwrap">
      <table class="dt">
        <thead>
          <tr>
            <For each={props.columns}>{(c) => <th style={c.width ? `width:${c.width}` : ""}>{c.label}</th>}</For>
          </tr>
        </thead>
        <tbody>
          <Show when={props.rows.length > 0} fallback={
            <tr><td colSpan={props.columns.length}>
              <EmptyState icon={props.emptyIcon ?? "table"} title={props.emptyTitle} body={props.empty} />
            </td></tr>
          }>
            <For each={props.rows}>{(row) => (
              <tr
                classList={{ sel: props.selectedKey?.() === props.rowKey(row) }}
                onClick={() => props.onRowClick?.(row)}
                onContextMenu={(e) => props.onRowContextMenu?.(row, e)}
              >
                <For each={props.columns}>{(c) => (
                  <td class={c.mono ? "m" : ""}>{c.render ? c.render(row) : String((row as any)[c.key] ?? "")}</td>
                )}</For>
              </tr>
            )}</For>
          </Show>
        </tbody>
      </table>
    </div>
  );
}
