import { For, Show } from "solid-js";
import { ConsoleView } from "./ConsoleView";
import { InteractView } from "../views/InteractView";
import { SessionsView } from "../views/SessionsView";
import { ListenersView } from "../views/ListenersView";
import { JobsView } from "../views/JobsView";
import { OperationsView } from "../views/OperationsView";
import { CredsView } from "../views/CredsView";
import { LootView } from "../views/LootView";
import { EventsView } from "../views/EventsView";
import { consoleBusy, consoleOutput, consolePrompt, tabs as consoleTabs, write } from "../stores/console";
import { unreadChat } from "../stores/events";
import { flash } from "../statusflash";
import { errorMessage } from "../errors";

const notebookTabs: [string, string, string][] = [
  ["console", "terminal-window", "Console"],
  ["interact", "terminal", "Interact"],
  ["sessions", "broadcast", "Sessions"],
  ["listeners", "plug", "Listeners"],
  ["jobs", "gear", "Jobs"],
  ["ops", "pulse", "Operations"],
  ["creds", "key", "Credentials"],
  ["loot", "archive", "Loot"],
  ["events", "chat", "Events"],
];

export function NotebookDock(props: {
  tab: () => string;
  onTab: (id: string) => void;
  liveCount: () => number;
  jobCount: () => number;
  runningOps: () => number;
  maximized: () => boolean;
  onToggleMaximize: () => void;
  onInteract: (sid: string) => void;
  onScanSubnet: (subnet: string) => void;
}) {
  return (
    <section class="nb card">
      <div class="nbtabs">
        <For each={notebookTabs}>{([id, icon, label]) => (
          <button class="nbtab" aria-pressed={props.tab() === id} classList={{ on: props.tab() === id }} onClick={() => props.onTab(id)}>
            <i aria-hidden="true" class={`ph ph-${icon}`}></i>{label}
            <Show when={id === "sessions" && props.liveCount() > 0}>
              <span class="badge">{props.liveCount()}</span>
            </Show>
            <Show when={id === "jobs" && props.jobCount() > 0}>
              <span class="badge jobs">{props.jobCount()}</span>
            </Show>
            <Show when={id === "ops" && props.runningOps() > 0}>
              <span class="badge jobs" title="Running operations">{props.runningOps()}</span>
            </Show>
            <Show when={id === "events" && unreadChat() > 0}>
              <span class="badge chat" title="Unread team chat">{unreadChat()}</span>
            </Show>
          </button>
        )}</For>
        <span class="spacer"></span>
        <button class="ibtn" style="align-self:center; margin-right:4px" aria-pressed={props.maximized()}
          onClick={props.onToggleMaximize}
          aria-label={props.maximized() ? "Restore bottom dock" : "Maximize bottom dock"}
          title={props.maximized() ? "Restore bottom dock" : "Maximize bottom dock"}>
          <i aria-hidden="true" class="ph ph-arrows-out-simple"></i>
        </button>
      </div>
      <div class="nbbody">
        <div class="nbpane" hidden={props.tab() !== "console"}>
          <ConsoleView output={consoleOutput} prompt={consolePrompt()} busy={consoleBusy()}
            write={(cmd) => void write(cmd).catch((e) => flash(errorMessage(e) || "console write failed"))}
            tabComplete={(line) => consoleTabs(line).catch(() => [])} />
        </div>
        <div class="nbpane" hidden={props.tab() !== "interact"}><InteractView /></div>
        <div class="nbpane" hidden={props.tab() !== "sessions"}>
          <SessionsView onInteract={props.onInteract} active={() => props.tab() === "sessions"}
            onScanSubnet={props.onScanSubnet} />
        </div>
        <div class="nbpane" hidden={props.tab() !== "listeners"}>
          <ListenersView onInteract={props.onInteract} active={() => props.tab() === "listeners"} />
        </div>
        <div class="nbpane" hidden={props.tab() !== "jobs"}>
          <JobsView active={() => props.tab() === "jobs"} onOpenModule={(module) => {
            props.onTab("console");
            void write(`use ${module}`)
              .catch((e) => flash(errorMessage(e) || "console write failed"));
          }} />
        </div>
        <div class="nbpane" hidden={props.tab() !== "ops"}>
          <OperationsView active={() => props.tab() === "ops"} />
        </div>
        <div class="nbpane" hidden={props.tab() !== "creds"}><CredsView /></div>
        <div class="nbpane" hidden={props.tab() !== "loot"}><LootView /></div>
        <div class="nbpane" hidden={props.tab() !== "events"}><EventsView /></div>
      </div>
    </section>
  );
}
