import { Show, createSignal } from "solid-js";
import { DataTable } from "../components/DataTable";
import { Modal } from "../components/modal";
import { openContextMenuFor } from "../components/contextmenu";
import { copyWithFeedback } from "../clipboard";
import { campaignState } from "../stores/store";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import { flash } from "../statusflash";
import type { CredState } from "../protocol/types";
import { LoginDialog } from "./LoginDialog";

export function CredsView() {
  const rows = () => campaignState().creds.filter((c): c is CredState => !!c);
  const [adding, setAdding] = createSignal(false);
  const [loginHost, setLoginHost] = createSignal<string | undefined>(undefined);
  const [host, setHost] = createSignal("");
  const [port, setPort] = createSignal("22");
  const [service, setService] = createSignal("ssh");
  const [user, setUser] = createSignal("");
  const [pass, setPass] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  async function addCred() {
    setBusy(true);
    setError("");
    try {
      await ws.command("db.cred_add", {
        host: host().trim(), port: Number(port()) || 0,
        service: service().trim(), user: user().trim(), pass: pass(),
      });
      setAdding(false);
    } catch (e) {
      setError(e instanceof CommandError ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
    <div class="pane-bar end">
      <button class="tbtn" onClick={() => setAdding(true)}>
        <i aria-hidden="true" class="ph ph-plus"></i>Add credential
      </button>
    </div>
    <DataTable
      rows={rows()}
      rowKey={(r) => `${r.host}:${r.port}:${r.user}:${r.pass}`}
      emptyTitle="No credentials"
      emptyIcon="key"
      empty="Recovered passwords and hashes appear here."
      onRowContextMenu={(r, e) => {
        openContextMenuFor(e, [
          { head: r.user || "(no user)", sub: `recovered credential on ${r.host}` },
          { sep: true },
          { icon: "copy", label: "Copy value", fn: () => copyWithFeedback(r.pass) },
          { icon: "copy", label: "Copy user", fn: () => copyWithFeedback(r.user) },
          { sep: true },
          { icon: "key", label: "Login with this host's creds…", fn: () => setLoginHost(r.host) },
          { icon: "x", label: "Delete credential", danger: true, fn: () => {
            ws.command("db.cred_delete", { host: r.host, port: r.port, service: r.service, user: r.user })
              .catch((e: any) => flash(e?.message ?? "credential delete failed"));
          } },
        ]);
      }}
      columns={[
        { key: "host", label: "Host", mono: true },
        { key: "user", label: "User", render: (r) => <b>{r.user}</b> },
        { key: "type", label: "Type", mono: true, render: (r) => <span class="dim">{r.type || "password"}</span> },
        { key: "pass", label: "Value", mono: true },
        { key: "service", label: "Service", render: (r) => <span class="dim">{r.service || ""}</span> },
      ]}
    />
    <Show when={adding()}>
      <Modal title="Add credential" onClose={() => setAdding(false)} width="440px">
        <div class="mform">
          <label class="field"><b>Host</b>
            <input value={host()} onInput={(e) => setHost(e.currentTarget.value)} placeholder="10.0.0.5" autocomplete="off" /></label>
          <div class="frow even">
            <label class="field"><b>Port</b>
              <input value={port()} inputmode="numeric" onInput={(e) => setPort(e.currentTarget.value)} /></label>
            <label class="field"><b>Service</b>
              <input value={service()} onInput={(e) => setService(e.currentTarget.value)} autocomplete="off" /></label>
          </div>
          <label class="field"><b>User</b>
            <input value={user()} onInput={(e) => setUser(e.currentTarget.value)} autocomplete="off" /></label>
          <label class="field"><b>Password or hash</b>
            <input value={pass()} onInput={(e) => setPass(e.currentTarget.value)} autocomplete="off" /></label>
        </div>
        <Show when={error()}><p class="merror">{error()}</p></Show>
        <div class="mbtns">
          <button class="abtn" disabled={busy() || !host().trim() || !user().trim()}
            onClick={() => void addCred()}>{busy() ? "Adding…" : "Add credential"}</button>
        </div>
      </Modal>
    </Show>
    <Show when={loginHost()}>
      {(h) => <LoginDialog host={h()} onClose={() => setLoginHost(undefined)} />}
    </Show>
    </>
  );
}
