import { Show, createEffect, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { FilterSelect } from "../components/FilterSelect";
import { Modal } from "../components/modal";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState } from "../stores/store";
import { createNowSignal } from "../stores/now";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { flash } from "../statusflash";
import { copyWithFeedback } from "../clipboard";
import { ageOf } from "./format";
import { jobRows, type JobRow } from "./jobs";
import { PayloadDialog } from "./PayloadDialog";
import type { SessionState } from "../protocol/types";

// A handler job only names its module; the payload and callback address are
// gone by the time job.list answers. The launch details are kept here so the
// table can show what each listener was started with.
interface ListenerLaunch {
  jobId: string;
  payload: string;
  lhost: string;
  lport: number;
}

function loadLaunches(): ListenerLaunch[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem("hayduk.listeners") ?? "[]");
    return Array.isArray(parsed) ? parsed.filter((v): v is ListenerLaunch =>
      !!v && typeof v === "object" && typeof (v as ListenerLaunch).payload === "string") : [];
  } catch {
    return [];
  }
}

export function ListenersView(props: { onInteract: (sid: string) => void }) {
  const now = createNowSignal(10_000);
  const [launches, setLaunches] = createSignal<ListenerLaunch[]>(loadLaunches());
  const [starting, setStarting] = createSignal(false);
  const [showGenerate, setShowGenerate] = createSignal(false);
  const [payload, setPayload] = createSignal("");
  const [lhost, setLhost] = createSignal("");
  const [lport, setLport] = createSignal("4444");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const payloads = () => (campaignState().modules?.payloads ?? [])
    .filter(p => p.includes("meterpreter") || p.includes("shell"));
  createEffect(() => {
    if (!payload() && payloads().length > 0) {
      setPayload(payloads().find(p => p.includes("linux/x64/meterpreter/reverse_tcp")) ?? payloads()[0]!);
    }
  });

  const handlers = () => jobRows(campaignState().jobs).filter(r => r.module.includes("multi/handler"));
  const handlerSessions = () => Object.values(campaignState().sessions)
    .filter((s): s is SessionState => !!s && (s.viaExploit ?? "").includes("multi/handler"));
  const launchFor = (jobId: string) => launches().find(l => l.jobId === jobId);
  // a launch knows its payload but not its job's sessions; the payload is as
  // close to a link as the daemon's job and session records come
  const sessionsFor = (job: JobRow) => handlerSessions()
    .filter(s => {
      const launch = launchFor(job.id);
      return !launch || !s.viaPayload || s.viaPayload === launch.payload;
    });

  function saveLaunches(next: ListenerLaunch[]) {
    setLaunches(next);
    try {
      localStorage.setItem("hayduk.listeners", JSON.stringify(next.slice(-24)));
    } catch { /* storage unavailable */ }
  }

  const valid = () => payload() !== "" && lhost().trim() !== "" && Number(lport()) > 0;

  async function start() {
    setBusy(true);
    setError("");
    try {
      const res = await ws.command<{ jobId: number; uuid: string }>("module.execute", {
        type: "exploit", name: "multi/handler",
        options: { ExitOnSession: false },
        payload: payload(),
        payloadOptions: { LHOST: lhost().trim(), LPORT: Number(lport()) },
      });
      saveLaunches([...launches(), {
        jobId: String(res.jobId), payload: payload(),
        lhost: lhost().trim(), lport: Number(lport()),
      }]);
      setStarting(false);
      flash(`listener starting for ${payload()}`);
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function stop(jobId: string) {
    try {
      await ws.command("job.stop", { id: jobId });
      saveLaunches(launches().filter(l => l.jobId !== jobId));
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
        rows={handlers()}
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
