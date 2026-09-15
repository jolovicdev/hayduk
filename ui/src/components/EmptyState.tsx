import { JSX, Show } from "solid-js";

export function EmptyState(props: { icon: string; title: string; body?: string; action?: JSX.Element }) {
  return (
    <div class="empty-state">
      <div class="empty-symbol"><i aria-hidden="true" class={`ph ph-${props.icon}`}></i></div>
      <h2>{props.title}</h2>
      <Show when={props.body}><p>{props.body}</p></Show>
      <Show when={props.action}>{props.action}</Show>
    </div>
  );
}
