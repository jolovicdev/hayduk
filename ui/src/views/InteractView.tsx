import { For, Show, createSignal } from "solid-js";
import { ConsoleView } from "../components/ConsoleView";
import { EmptyState } from "../components/EmptyState";
import {
  activateTab, detach, interactOutput, interactSID, interactTabs, write,
} from "../stores/interact";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { interactPrompt } from "./format";
import { parseFiles, parseProcesses, type FileRow, type ProcessRow } from "./sessionTools";

export function InteractView() {
  const session = () => campaignState().sessions[interactSID()];
  const isMeterpreter = () => session()?.type === "meterpreter";
  const placeholder = () => isMeterpreter()
    ? "type a meterpreter command"
    : "type a shell command";

  const [toolsOpen, setToolsOpen] = createSignal(false);
  const [toolBusy, setToolBusy] = createSignal("");
  const [toolOutput, setToolOutput] = createSignal("");
  const [processes, setProcesses] = createSignal<ProcessRow[]>([]);
  const [files, setFiles] = createSignal<FileRow[]>([]);
  const [filePath, setFilePath] = createSignal(".");

  async function run(label: string, command: string) {
    const sid = interactSID();
    if (!sid) return;
    setToolBusy(label);
    try {
      const res = await ws.command<{ output: string }>("session.run", { sid, command });
      setToolOutput(res.output || "(no output)");
      if (label === "Processes") setProcesses(parseProcesses(res.output));
      if (label === "Files") setFiles(parseFiles(res.output));
    } catch (e: any) {
      flash(e?.message ?? `${label} failed`);
    } finally {
      setToolBusy("");
    }
  }

  async function hashdump() {
    const sid = interactSID();
    try {
      await ws.command("module.execute", {
        type: "post", name: "windows/gather/hashdump", options: { SESSION: sid },
      });
      flash("hashdump launched; results land in Credentials and the event log");
    } catch (e: any) {
      flash(e?.message ?? "hashdump failed");
    }
  }

  return (
    <Show when={interactTabs().length > 0} fallback={
      <div class="console">
        <EmptyState icon="terminal-window" title="No session attached" body="Right-click a session or host and choose Interact." />
      </div>
    }>
      <div class="session-tabs" role="tablist" aria-label="Open session consoles">
        <For each={interactTabs()}>{(tabSid) => (
          <span class="stab" classList={{ on: tabSid === interactSID() }}>
            <button class="stab-main" role="tab" aria-selected={tabSid === interactSID()}
              onClick={() => activateTab(tabSid)}>
              Session {tabSid} · {campaignState().sessions[tabSid]?.type ?? "gone"}
            </button>
            <button class="stab-close" aria-label={`Close session ${tabSid} console`}
              onClick={() => void detach(tabSid).catch((e: any) => flash(e?.message ?? "detach failed"))}>
              <i aria-hidden="true" class="ph ph-x"></i>
            </button>
          </span>
        )}</For>
      </div>
      <div class="sess-head">
        <i aria-hidden="true" class="ph ph-broadcast live"></i>
        <span class="who">
          session {interactSID()} · {session()?.type ?? ""} · {session()?.username || session()?.targetHost || ""}
        </span>
        <span class="spacer"></span>
        <Show when={isMeterpreter()}>
          <button class="abtn" aria-pressed={toolsOpen()}
            onClick={() => setToolsOpen(!toolsOpen())}>
            <i class="ph ph-gear"></i>Tools
          </button>
        </Show>
        <button class="abtn"
          onClick={() => void detach().catch((e: any) => flash(e?.message ?? "detach failed"))}>
          <i class="ph ph-x"></i>Detach
        </button>
      </div>
      <Show when={toolsOpen() && isMeterpreter()}>
        <div class="tools-panel">
          <div class="tools-actions">
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Sysinfo", "sysinfo")}>Sysinfo</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Processes", "ps")}>Processes</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Screenshot", "screenshot")}>Screenshot</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Keylog", "keylog_start")}>Keylog start</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Keylog dump", "keylog_dump")}>Keylog dump</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void hashdump()}>Hashdump</button>
          </div>
          <div class="tools-actions">
            <input value={filePath()} onInput={(e) => setFilePath(e.currentTarget.value)}
              placeholder="remote path, e.g. C:\Users" aria-label="Remote path" />
            <button class="abtn" disabled={!!toolBusy()}
              onClick={() => void run("Files", `ls ${filePath() || "."}`)}>List files</button>
          </div>
          <Show when={processes().length > 0}>
            <div class="tools-table">
              <For each={processes()}>{(p) => (
                <div class="tools-row">
                  <span class="m">{p.pid}</span><span>{p.name}</span>
                  <span class="dim">{p.detail}</span>
                  <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Kill", `kill ${p.pid}`)}>Kill</button>
                  <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Migrate", `migrate ${p.pid}`)}>Migrate</button>
                </div>
              )}</For>
            </div>
          </Show>
          <Show when={files().length > 0}>
            <div class="tools-table">
              <For each={files()}>{(f) => (
                <div class="tools-row">
                  <span>{f.name}</span><span class="dim">{f.kind} · {f.size}</span>
                  <Show when={f.kind !== "dir"}>
                    <button class="abtn" disabled={!!toolBusy()}
                      onClick={() => void run("Download", `download ${filePath()}/${f.name}`)}>Download</button>
                  </Show>
                </div>
              )}</For>
            </div>
          </Show>
          <Show when={toolOutput()}>
            <pre class="toolout">{toolBusy() ? `${toolBusy()}…\n` : ""}{toolOutput()}</pre>
          </Show>
        </div>
      </Show>
      <ConsoleView output={interactOutput}
        prompt={interactPrompt(session()?.type, interactSID())} busy={false}
        target={interactSID}
        placeholder={placeholder()}
        write={(cmd, target) => {
          void write(cmd, target ?? interactSID())
            .catch((e: any) => flash(e?.message ?? "session write failed"));
        }}
        tabComplete={async () => []} />
    </Show>
  );
}
