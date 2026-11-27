import { For, Show, createEffect, createSignal, on } from "solid-js";
import { ConsoleView } from "../components/ConsoleView";
import { EmptyState } from "../components/EmptyState";
import { FilterSelect } from "../components/FilterSelect";
import { LaunchDialog } from "./LaunchDialog";
import {
  activateTab, detach, interactOutput, interactSID, interactTabs, write,
} from "../stores/interact";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { errorMessage } from "../errors";
import { interactPrompt } from "./format";
import { joinRemote, parentRemote, parseProcesses, type ProcessRow } from "./sessionTools";
import { base64ToBytes, bytesToBase64, downloadBytes } from "../bytes";
import type { FileTransferPayload, FsEntry, FsListPayload } from "../protocol/types";

export function InteractView() {
  const session = () => campaignState().sessions[interactSID()];
  const isMeterpreter = () => session()?.type === "meterpreter";
  const localFiles = () => campaignState().connection.fileStaging === "local";
  const placeholder = () => isMeterpreter()
    ? "type a meterpreter command"
    : "type a shell command";

  const [toolsOpen, setToolsOpen] = createSignal(false);
  const [toolBusy, setToolBusy] = createSignal("");
  const [toolOutput, setToolOutput] = createSignal("");
  const [processes, setProcesses] = createSignal<ProcessRow[]>([]);
  const [shot, setShot] = createSignal<{ data: string; filename: string } | null>(null);

  const [fsPath, setFsPath] = createSignal("");
  const [fsDraft, setFsDraft] = createSignal("");
  const [fsSeparator, setFsSeparator] = createSignal("/");
  const [fsEntries, setFsEntries] = createSignal<FsEntry[]>([]);
  const [fsLoaded, setFsLoaded] = createSignal(false);

  const [postPickerOpen, setPostPickerOpen] = createSignal(false);
  const [postModules, setPostModules] = createSignal<string[]>([]);
  const [postChoice, setPostChoice] = createSignal("");
  const [postLaunch, setPostLaunch] = createSignal<string | undefined>(undefined);

  let uploadEl!: HTMLInputElement;

  // a new session tab starts with a fresh tools view: the old filesystem
  // and screenshot belong to the session they came from
  createEffect(on(interactSID, () => {
    setFsLoaded(false);
    setFsEntries([]);
    setFsPath("");
    setFsDraft("");
    setShot(null);
    setProcesses([]);
    setToolOutput("");
    setPostPickerOpen(false);
  }, { defer: true }));

  async function run(label: string, command: string) {
    const sid = interactSID();
    if (!sid) return;
    setToolBusy(label);
    try {
      const res = await ws.command<{ output: string }>("session.run", { sid, command });
      setToolOutput(res.output || "(no output)");
      if (label === "Processes") setProcesses(parseProcesses(res.output));
    } catch (e) {
      flash(errorMessage(e) || `${label} failed`);
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
    } catch (e) {
      flash(errorMessage(e) || "hashdump failed");
    }
  }

  async function suggester() {
    const sid = interactSID();
    if (!sid) return;
    try {
      await ws.command("session.post", { sid, module: "multi/recon/local_exploit_suggester" });
      flash("exploit suggester running; output streams in the session console");
    } catch (e) {
      flash(errorMessage(e) || "exploit suggester failed");
    }
  }

  async function screenshot() {
    const sid = interactSID();
    if (!sid) return;
    setToolBusy("Screenshot");
    try {
      const res = await ws.command<FileTransferPayload>("session.screenshot", { sid });
      if (res.dataBase64) {
        setShot({ data: res.dataBase64, filename: res.filename || "screenshot.jpeg" });
        setToolOutput("");
      } else {
        setToolOutput(res.savedPath
          ? `Screenshot saved on the msfrpcd host at ${res.savedPath}`
          : res.output || "(no output)");
      }
    } catch (e) {
      flash(errorMessage(e) || "screenshot failed");
    } finally {
      setToolBusy("");
    }
  }

  async function loadDir(path?: string) {
    const sid = interactSID();
    if (!sid) return;
    setToolBusy("Files");
    try {
      const res = await ws.command<FsListPayload>("session.fs_list", { sid, path: path ?? "" });
      setFsPath(res.path);
      setFsDraft(res.path);
      setFsSeparator(res.separator || "/");
      setFsEntries(res.entries ?? []);
      setFsLoaded(true);
    } catch (e) {
      flash(errorMessage(e) || "directory listing failed");
    } finally {
      setToolBusy("");
    }
  }

  async function downloadFile(name: string) {
    const sid = interactSID();
    if (!sid) return;
    setToolBusy("Download");
    try {
      const res = await ws.command<FileTransferPayload>("session.file_download", {
        sid, path: joinRemote(fsPath(), name, fsSeparator()),
      });
      if (res.dataBase64) {
        downloadBytes(res.filename || name, base64ToBytes(res.dataBase64));
        flash(`downloaded ${res.filename || name}`);
      } else {
        setToolOutput(res.output || "");
        flash(res.savedPath
          ? `saved on the msfrpcd host at ${res.savedPath}`
          : "download finished on the msfrpcd host");
      }
    } catch (e) {
      flash(errorMessage(e) || "download failed");
    } finally {
      setToolBusy("");
    }
  }

  async function uploadFiles(files: FileList | null) {
    const sid = interactSID();
    if (!sid || !files || files.length === 0) return;
    setToolBusy("Upload");
    try {
      for (const file of Array.from(files)) {
        const bytes = new Uint8Array(await file.arrayBuffer());
        await ws.command("session.file_upload", {
          sid,
          path: joinRemote(fsPath(), file.name, fsSeparator()),
          dataBase64: bytesToBase64(bytes),
        });
      }
      flash(`uploaded ${files.length} file${files.length === 1 ? "" : "s"} to ${fsPath()}`);
      await loadDir(fsPath());
    } catch (e) {
      flash(errorMessage(e) || "upload failed");
    } finally {
      setToolBusy("");
      if (uploadEl) uploadEl.value = "";
    }
  }

  async function openPostPicker() {
    const sid = interactSID();
    if (!sid) return;
    setPostPickerOpen(true);
    setPostChoice("");
    try {
      const res = await ws.command<{ modules: string[] }>("session.post_modules", { sid });
      setPostModules(res.modules ?? []);
    } catch {
      setPostModules(campaignState().modules?.post ?? []);
    }
  }

  function toggleTools() {
    const next = !toolsOpen();
    setToolsOpen(next);
    if (next && isMeterpreter() && !fsLoaded()) void loadDir("");
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
          <button class="abtn" aria-pressed={toolsOpen()} onClick={toggleTools}>
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
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Getsystem", "getsystem")}>Getsystem</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void suggester()}>Exploit suggester</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void screenshot()}>Screenshot</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Keylog", "keylog_start")}>Keylog start</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void run("Keylog dump", "keylog_dump")}>Keylog dump</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void hashdump()}>Hashdump</button>
            <button class="abtn" disabled={!!toolBusy()} onClick={() => void openPostPicker()}>Post module…</button>
          </div>

          <Show when={postPickerOpen()}>
            <div class="tools-actions">
              <FilterSelect label="post modules" options={postModules().map(m => ({ value: m }))}
                value={postChoice()} onChange={setPostChoice} />
              <button class="abtn primary" disabled={!postChoice()}
                onClick={() => { setPostLaunch(postChoice()); setPostPickerOpen(false); }}>
                Configure…
              </button>
              <button class="abtn" onClick={() => setPostPickerOpen(false)}>Cancel</button>
            </div>
          </Show>

          <Show when={shot()}>
            {(s) => (
              <div class="shot-view">
                <img src={`data:image/jpeg;base64,${s().data}`} alt={`Screenshot from session ${interactSID()}`} />
                <div class="tools-actions">
                  <span class="dim">{s().filename}</span>
                  <button class="abtn" onClick={() => downloadBytes(s().filename, base64ToBytes(s().data), "image/jpeg")}>
                    <i aria-hidden="true" class="ph ph-download-simple"></i>Save
                  </button>
                  <button class="abtn" onClick={() => setShot(null)}>Dismiss</button>
                </div>
              </div>
            )}
          </Show>

          <div class="tools-fs">
            <div class="tools-actions">
              <input value={fsDraft()} onInput={(e) => setFsDraft(e.currentTarget.value)}
                onKeyDown={(e) => { if (e.key === "Enter") void loadDir(fsDraft()); }}
                placeholder="remote path, e.g. C:\Users" aria-label="Remote path" />
              <button class="abtn" disabled={!!toolBusy()} onClick={() => void loadDir(fsDraft())}>Go</button>
              <button class="abtn" disabled={!!toolBusy() || !fsLoaded()}
                onClick={() => void loadDir(parentRemote(fsPath(), fsSeparator()))}>
                <i aria-hidden="true" class="ph ph-arrow-up"></i>Up
              </button>
              <Show when={localFiles()}>
                <button class="abtn" disabled={!!toolBusy() || !fsLoaded()} onClick={() => uploadEl?.click()}>
                  <i aria-hidden="true" class="ph ph-upload-simple"></i>Upload
                </button>
                <input ref={uploadEl} type="file" multiple hidden aria-label="Upload files to the remote directory"
                  onChange={(e) => void uploadFiles(e.currentTarget.files)} />
              </Show>
            </div>
            <Show when={!localFiles()}>
              <p class="fs-note">
                msfrpcd is remote: downloads land on its disk and the path is reported here.
                Browsing, screenshots-as-path, and payload downloads still work.
              </p>
            </Show>
            <Show when={fsLoaded()}>
              <div class="tools-table" role="list" aria-label={`Files in ${fsPath()}`}>
                <For each={fsEntries()} fallback={
                  <div class="tools-row"><span class="dim">empty directory</span></div>
                }>{(f) => (
                  <div class="tools-row" role="listitem">
                    <Show when={f.kind === "dir"} fallback={
                      <span class="fsname"><i aria-hidden="true" class="ph ph-file"></i>{f.name}</span>
                    }>
                      <button class="fsdir" onClick={() => void loadDir(joinRemote(fsPath(), f.name, fsSeparator()))}>
                        <i aria-hidden="true" class="ph ph-folder"></i>{f.name}
                      </button>
                    </Show>
                    <span class="dim">{f.kind === "dir" ? "dir" : `${f.size} B`}</span>
                    <Show when={f.kind !== "dir"}>
                      <button class="abtn" disabled={!!toolBusy()} onClick={() => void downloadFile(f.name)}>
                        <i aria-hidden="true" class="ph ph-download-simple"></i>Download
                      </button>
                    </Show>
                  </div>
                )}</For>
              </div>
            </Show>
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
      <Show when={postLaunch()}>
        {(path) => (
          <LaunchDialog type="post" path={path()} prefillSession={interactSID()}
            onClose={() => setPostLaunch(undefined)} />
        )}
      </Show>
    </Show>
  );
}
