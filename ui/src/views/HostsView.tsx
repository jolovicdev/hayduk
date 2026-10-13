import { Show, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { Modal } from "../components/modal";
import { openContextMenuFor } from "../components/contextmenu";
import { campaignState, credsByHostMemo, sessionsByHostMemo } from "../stores/store";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { flash } from "../statusflash";
import type { HostState } from "../protocol/types";

export function HostsView(props: { onInspect: (addr: string) => void; selected?: string }) {
  const rows = () => campaignState().hosts.filter((h): h is HostState => !!h);
  const serviceCounts = () => {
    const counts = new Map<string, number>();
    for (const s of campaignState().services) {
      if (s) counts.set(s.host, (counts.get(s.host) ?? 0) + 1);
    }
    return counts;
  };
  const [adding, setAdding] = createSignal(false);
  const [importing, setImporting] = createSignal(false);
  const [noteFor, setNoteFor] = createSignal<HostState | null>(null);
  const [address, setAddress] = createSignal("");
  const [name, setName] = createSignal("");
  const [osName, setOsName] = createSignal("");
  const [note, setNote] = createSignal("");
  const [importData, setImportData] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  async function submit(method: string, params: unknown, close: () => void) {
    setBusy(true);
    setError("");
    try {
      await ws.command(method, params);
      close();
    } catch (e) {
      setError(e instanceof CommandError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="svcwrap">
      <div class="pane-bar end">
        <button class="tbtn" onClick={() => setImporting(true)}>
          <i aria-hidden="true" class="ph ph-download-simple"></i>Import scan
        </button>
        <button class="tbtn primary" onClick={() => setAdding(true)}>
          <i aria-hidden="true" class="ph ph-plus"></i>Add host
        </button>
      </div>
      <DataTable
        rows={rows()}
        rowKey={(r) => r.address}
        selectedKey={() => props.selected}
        emptyTitle="No hosts"
        emptyIcon="graph"
        empty="Discover hosts, import a scan, or add a host by hand."
        onRowClick={(r) => props.onInspect(r.address)}
        onRowContextMenu={(r, e) => {
          openContextMenuFor(e, [
            { head: r.name || r.address, sub: r.address },
            { icon: "info", label: "Inspect host", fn: () => props.onInspect(r.address) },
            { icon: "file-code", label: "Edit note…", fn: () => { setNote(r.comments ?? ""); setNoteFor(r); } },
            { sep: true },
            { icon: "x", label: "Delete host", danger: true, fn: () => {
              ws.command("db.host_delete", { address: r.address })
                .catch((err: any) => flash(err?.message ?? "host delete failed"));
            } },
          ]);
        }}
        columns={[
          { key: "address", label: "Host", mono: true, render: (r) => <b>{r.address}</b> },
          { key: "name", label: "Name" },
          { key: "osName", label: "OS", render: (r) => <span class="dim">{[r.osName, r.osFlavor, r.osVersion].filter(Boolean).join(" ") || "unknown"}</span> },
          { key: "services", label: "Services", mono: true, render: (r) => String(serviceCounts().get(r.address) ?? 0) },
          { key: "access", label: "Access", render: (r) => {
            const sessions = sessionsByHostMemo().get(r.address)?.length ?? 0;
            const creds = credsByHostMemo().get(r.address)?.length ?? 0;
            return sessions > 0 ? <span class="st">{sessions} session{sessions === 1 ? "" : "s"}</span>
              : creds > 0 ? <span class="amb">login found</span>
              : <span class="dim">none</span>;
          } },
        ]}
      />
      <Show when={adding()}>
        <Modal title="Add host" onClose={() => setAdding(false)} width="440px">
          <div class="mform">
            <label class="field"><b>Address</b>
              <input value={address()} onInput={(e) => setAddress(e.currentTarget.value)} placeholder="10.0.0.5" autocomplete="off" /></label>
            <label class="field"><b>Name</b>
              <input value={name()} onInput={(e) => setName(e.currentTarget.value)} autocomplete="off" /></label>
            <label class="field"><b>Operating system</b>
              <input value={osName()} onInput={(e) => setOsName(e.currentTarget.value)} placeholder="Linux" autocomplete="off" /></label>
          </div>
          <Show when={error()}><p class="merror">{error()}</p></Show>
          <div class="mbtns">
            <button class="abtn" disabled={busy() || !address().trim()}
              onClick={() => void submit("db.host_add",
                { address: address().trim(), name: name().trim(), osName: osName().trim() },
                () => setAdding(false))}>
              {busy() ? "Adding…" : "Add host"}
            </button>
          </div>
        </Modal>
      </Show>
      <Show when={importing()}>
        <Modal title="Import scan results" onClose={() => setImporting(false)} width="560px">
          <p>Paste Nmap XML or a Nessus export. Metasploit imports hosts and services into the active workspace.</p>
          <textarea value={importData()} rows={10}
            onInput={(e) => setImportData(e.currentTarget.value)}
            placeholder="<nmaprun>…</nmaprun>" spellcheck={false} />
          <input type="file" accept=".xml,.nessus,.nmap" style="margin-top:10px"
            onChange={(e) => {
              const file = e.currentTarget.files?.[0];
              if (file) void file.text().then(setImportData);
            }} />
          <Show when={error()}><p class="merror">{error()}</p></Show>
          <div class="mbtns">
            <button class="abtn" disabled={busy() || !importData().trim()}
              onClick={() => void submit("db.import", { data: importData() }, () => setImporting(false))}>
              {busy() ? "Importing…" : "Import"}
            </button>
          </div>
        </Modal>
      </Show>
      <Show when={noteFor()}>
        {(h) => (
          <Modal title={`Note for ${h().address}`} onClose={() => setNoteFor(null)} width="460px">
            <textarea value={note()} rows={5}
              onInput={(e) => setNote(e.currentTarget.value)}
              placeholder="What should the next operator know about this host?" />
            <Show when={error()}><p class="merror">{error()}</p></Show>
            <div class="mbtns">
              <button class="abtn" disabled={busy()}
                onClick={() => void submit("db.host_note",
                  { address: h().address, comments: note() }, () => setNoteFor(null))}>
                {busy() ? "Saving…" : "Save note"}
              </button>
            </div>
          </Modal>
        )}
      </Show>
    </div>
  );
}
