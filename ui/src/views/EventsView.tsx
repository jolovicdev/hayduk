import { For, Show } from "solid-js";
import { eventGap, events } from "../stores/events";

export function EventsView() {
  return (
    <div class="events">
      <Show when={eventGap()}>
        <div class="evline">
          <time></time>
          <p class="err">event gap detected: events may be incomplete until reconnect</p>
        </div>
      </Show>
      <For each={events().slice().reverse()}>{(ev) => (
        <div class="evline">
          <time>{ev.time ? new Date(ev.time).toLocaleTimeString() : ""}</time>
          <p classList={{
            ok: ev.level === "success",
            err: ev.level === "error",
            warn: ev.level === "warn",
          }}>
            {ev.text}
            <Show when={ev.operator}><span class="opchip" title={`operator ${ev.operator}`}>{ev.operator}</span></Show>
          </p>
        </div>
      )}</For>
    </div>
  );
}
