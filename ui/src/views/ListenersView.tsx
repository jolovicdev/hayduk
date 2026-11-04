import { Show, createEffect, createMemo, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { FilterSelect } from "../components/FilterSelect";
import { Modal } from "../components/modal";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ws } from "../ws/singleton";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
import { ageOf } from "./format";
import { jobRows, type JobRow } from "./jobs";
import { PayloadDialog } from "./PayloadDialog";
import type { ListenerState, SessionState } from "../protocol/types";
import { errorMessage } from "../errors";
import { listenerRows } from "./listeners";

export function ListenersView(props: { onInteract: (sid: string) => void; active?: () => boolean }) {
  const now = createNowSignal(10_000, () => props.active?.() ?? true);
  const [starting, setStarting] = createSignal(false);
  const [showGenerate, setShowGenerate] = createSignal(false);
  const [payload, setPayload] = createSignal("");
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const modules = createMemo(() => campaignState().modules);
  const jobs = createMemo(() => campaignState().jobs);
  const sessions = createMemo(() => campaignState().sessions);
  const listeners = createMemo(() => campaignState().listeners);
  const payloads = createMemo(() => (modules()?.payloads ?? [])
    .filter(p => p.includes("meterpreter") || p.includes("shell")));
  createEffect(() => {
    if (!payload() && payloads().length > 0) {
      setPayload(payloads().find(p => p.includes("linux/x64/meterpreter/reverse_tcp")) ?? payloads()[0]!);
    }
  });

  const rows = createMemo(() => listenerRows(jobRows(jobs()), listeners()));
  const handlerSessions = createMemo(() => Object.values(sessions())
    .filter((s): s is SessionState => !!s && (s.viaExploit ?? "").includes("multi/handler")));
  const launchFor = (jobId: string) => listeners().find(l => l?.jobId === jobId);
  // a launch knows its payload but not its job's sessions; the payload is as
  // close to a link as the daemon's job and session records come
  const sessionsFor = (job: JobRow) => handlerSessions()
    .filter(s => {
      const launch = launchFor(job.id);
      return !launch || !s.viaPayload || s.viaPayload === launch.payload;
    });

  const valid = () => payload() !== "" && lhost().trim() !== "" && Number(lport()) > 0;

  async function start() {
    setBusy(true);
    setError("");
    try {
      await ws.command("module.execute", {
        type: "exploit", name: "multi/handler",
        options: { ExitOnSession: false },
        payload: payload(),
        payloadOptions: { LHOST: lhost().trim(), LPORT: Number(lport()) },
      });
      setStarting(false);
      flash(`listener starting for ${payload()}`);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function stop(jobId: string) {
    try {
      await ws.command("job.stop", { id: jobId });
    } catch (e: any) {
      flash(e?.message ?? `could not stop listener ${jobId}`);
    }
  }

  return (
    <>
      <div class="pane-bar end">
        <button class="tbtn" onClick={() => setShowGenerate(true)}>
          <i aria-hidden="true" class="ph ph-download-simple"></i>Generate payload
        </button>
        <button class="tbtn primary" onClick={() => setStarting(true)}>
          <i aria-hidden="true" class="ph ph-plus"></i>Start listener
        </button>
      </div>

      <DataTable
        rows={rows()}
        rowKey={(r) => r.id}
        emptyTitle="No listeners running"
        emptyIcon="plug"
        empty="Start a multi/handler listener, generate a matching payload, and sessions that call back land here and in Sessions."
        onRowContextMenu={(r, e) => {
          const launch = launchFor(r.id);
          openContextMenuFor(e, [
            { head: `Job ${r.id}`, sub: launch ? `${launch.payload} on ${launch.lhost}:${launch.lport}` : r.module },
            { icon: "copy", label: "Copy payload", fn: () => copyWithFeedback(launch?.payload ?? r.module) },
            ...(launch ? [{
              icon: "copy", label: "Copy callback address",
              fn: () => copyWithFeedback(`${launch.lhost}:${launch.lport}`),
            }] : []),
            { sep: true },
            { icon: "x", label: "Stop listener", danger: true, fn: () => void stop(r.id) },
          ]);
        }}
        columns={[
          { key: "id", label: "Job", mono: true, width: "56px" },
          { key: "payload", label: "Payload", mono: true, render: (r) =>
            launchFor(r.id)?.payload ?? <span class="dim">unknown</span> },
          { key: "endpoint", label: "Callback", mono: true, render: (r) => {
            const launch = launchFor(r.id);
            return launch ? `${launch.lhost}:${launch.lport}` : <span class="dim">-</span>;
          } },
          { key: "sessions", label: "Sessions", width: "88px", render: (r) => {
            const n = sessionsFor(r).length;
            return n > 0 ? <span class="st">{n} caught</span> : <span class="dim">none</span>;
          } },
          { key: "startedAt", label: "Running", mono: true, width: "90px",
            render: (r) => ageOf(r.startedAt, now()) },
          { key: "actions", label: "", width: "70px",
            render: (r) => <button class="abtn" onClick={() => void stop(r.id)}>Stop</button> },
        ]}
      />

      <Show when={handlerSessions().length > 0}>
        <div class="pane-section"><b>Sessions caught by listeners</b></div>
        <DataTable
          rows={handlerSessions()}
          rowKey={(s) => s.id}
          emptyTitle="No sessions"
          emptyIcon="broadcast"
          onRowClick={(s) => props.onInteract(s.id)}
          columns={[
            { key: "id", label: "Session", mono: true, width: "70px" },
            { key: "type", label: "Type", render: (s) => <b>{s.type}</b> },
            { key: "viaPayload", label: "Payload", mono: true,
              render: (s) => <span class="dim">{s.viaPayload || "-"}</span> },
            { key: "targetHost", label: "Host", mono: true,
              render: (s) => s.targetHost || s.sessionHost || "-" },
            { key: "openedAt", label: "Opened", mono: true, width: "90px",
              render: (s) => ageOf(s.openedAt, now()) },
            { key: "actions", label: "", width: "90px",
              render: (s) => <button class="abtn" onClick={() => props.onInteract(s.id)}>Interact</button> },
          ]}
        />
      </Show>

      <Show when={starting()}>
        <Modal title="Start listener" onClose={() => setStarting(false)} width="480px">
          <p class="mdesc">
            Runs exploit/multi/handler in the background with this payload and callback
            address. Generate the matching payload first, then run it on the target.
          </p>
          <div class="mform">
            <div class="field">
              <b>Payload</b>
              <FilterSelect label="payloads" options={payloads().map(p => ({ value: p }))}
                value={payload()} onChange={setPayload} />
            </div>
            <div class="frow">
              <label class="field"><b>LHOST</b>
                <input value={lhost()} placeholder="192.168.1.10" autocomplete="off" spellcheck={false}
                  onInput={(e) => setLhost(e.currentTarget.value)} />
              </label>
              <label class="field"><b>LPORT</b>
                <input value={lport()} inputmode="numeric"
                  onInput={(e) => setLport(e.currentTarget.value)} />
              </label>
            </div>
          </div>
          <Show when={error()}><p class="merror">{error()}</p></Show>
          <div class="mbtns">
            <button class="abtn" disabled={busy() || !valid()} onClick={() => void start()}>
              {busy() ? "Starting…" : "Start listener"}
            </button>
          </div>
        </Modal>
      </Show>

      <Show when={showGenerate()}>
        <PayloadDialog onClose={() => setShowGenerate(false)} />
      </Show>
    </>
  );
}
