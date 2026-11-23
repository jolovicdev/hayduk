import { For, Show, createSignal } from "solid-js";
import { eventGap, events } from "../stores/events";
import { campaignState, team as teamMode } from "../stores/store";
import { currentOperator } from "../ws/client";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { errorMessage } from "../errors";

// The shared event log is the team's coordination surface, Armitage-style:
// every operator's actions land here attributed, presence is visible, and
// the chat bar talks to everyone on the server.
export function EventsView() {
  const [draft, setDraft] = createSignal("");
  const [sending, setSending] = createSignal(false);

  async function sendChat() {
    const text = draft().trim();
    if (!text || sending()) return;
    setSending(true);
    try {
      await ws.command("team.chat", { text });
      setDraft("");
    } catch (e) {
      flash(errorMessage(e) || "chat message failed");
    } finally {
      setSending(false);
    }
  }

  return (
    <div class="events-wrap">
      <Show when={teamMode()}>
        <div class="team-strip" aria-label="Operators online">
          <i aria-hidden="true" class="ph ph-users-three"></i>
          <span class="team-label">team</span>
          <For each={campaignState().operators ?? []}>{(name) => (
            <span class="opchip live" classList={{ me: name === currentOperator() }}
              title={name === currentOperator() ? `${name} (you)` : name}>
              {name}
            </span>
          )}</For>
          <Show when={(campaignState().operators ?? []).length === 0}>
            <span class="team-empty">no operators online</span>
          </Show>
        </div>
      </Show>
      <div class="events">
        <Show when={eventGap()}>
          <div class="evline">
            <time></time>
            <p class="err">event gap detected: events may be incomplete until reconnect</p>
          </div>
        </Show>
        <For each={events()}>{(ev) => (
          <div class="evline">
            <time>{ev.time ? new Date(ev.time).toLocaleTimeString() : ""}</time>
            <Show when={ev.level === "chat"} fallback={
              <p classList={{
                ok: ev.level === "success",
                err: ev.level === "error",
                warn: ev.level === "warn",
              }}>
                {ev.text}
                <Show when={ev.operator}><span class="opchip" title={`operator ${ev.operator}`}>{ev.operator}</span></Show>
              </p>
            }>
              <p class="chat"><b>{ev.operator || "someone"}</b> {ev.text}</p>
            </Show>
          </div>
        )}</For>
      </div>
      <Show when={teamMode()}>
        <form class="chatbar" onSubmit={(e) => { e.preventDefault(); void sendChat(); }}>
          <input value={draft()} placeholder="Message the team…" maxlength={2000}
            aria-label="Team chat message" autocomplete="off" spellcheck={false}
            onInput={(e) => setDraft(e.currentTarget.value)} />
          <button class="abtn" type="submit" disabled={!draft().trim() || sending()}>
            <i aria-hidden="true" class="ph ph-paper-plane-tilt"></i>Send
          </button>
        </form>
      </Show>
    </div>
  );
}
