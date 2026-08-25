import { For, Show, createResource, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import {
  collectOptions, compatiblePayloadsParams, defaultText, launchDisabled, missingLaunchOptions, optionKind,
} from "./launchOptions";
import type { ModuleInfoPayload, ModuleOptionPayload } from "../protocol/types";

type OptionsMap = Record<string, ModuleOptionPayload>;

// defaultsFor spreads an option map's defaults as the edit base so required
// options satisfied by a default do not block the launch.
function defaultsFor(map: OptionsMap): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, def] of Object.entries(map)) {
    if (!def) continue;
    if (optionKind(def) === "bool") {
      out[name] = def.default === true;
    } else {
      out[name] = defaultText(def.default);
    }
  }
  return out;
}

function OptionField(props: {
  name: string;
  def: ModuleOptionPayload;
  value: () => unknown;
  onEdit: (v: unknown) => void;
}) {
  const kind = () => optionKind(props.def);
  return (
    <label style="display:grid; gap:4px">
      <span style="font:500 11px var(--sans); color:var(--tx1)">
        {props.name}
        <Show when={props.def.required}><span style="color:var(--red-br)"> *</span></Show>
        <span style="color:var(--tx2)"> ({props.def.desc})</span>
      </span>
      <Show when={kind() === "bool"} fallback={
        <Show when={kind() === "enum"} fallback={
          <input value={String(props.value() ?? "")}
            inputMode={kind() === "number" ? "numeric" : undefined}
            onInput={(e) => props.onEdit(e.currentTarget.value)} />
        }>
          <select value={String(props.value() ?? "")}
            onChange={(e) => props.onEdit(e.currentTarget.value)}>
            <For each={props.def.enums ?? []}>{(en) => <option value={en}>{en}</option>}</For>
          </select>
        </Show>
      }>
        <input type="checkbox" checked={props.value() === true || props.value() === "true"}
          onChange={(e) => props.onEdit(e.currentTarget.checked)} />
      </Show>
    </label>
  );
}

export function LaunchDialog(props: {
  type: string;
  path: string;
  prefillHost?: string;
  onClose: () => void;
}) {
  const [info] = createResource(async () =>
    ws.command<ModuleInfoPayload>("module.info", { type: props.type, name: props.path }));
  const [options] = createResource(async () =>
    ws.command<OptionsMap>("module.options", { type: props.type, name: props.path }));
  const [payloads] = createResource(async () => {
    const params = compatiblePayloadsParams(props.type, props.path);
    return params ? ws.command<string[]>("module.compatible_payloads", params) : [];
  });
  const [payload, setPayload] = createSignal("");
  const [payloadOptions] = createResource(payload, async (p) => {
    if (!p) return null;
    try {
      return await ws.command<OptionsMap>("module.options", { type: "payload", name: p });
    } catch {
      return null; // payload options are a convenience; the launch still works
    }
  });
  const [values, setValues] = createSignal<Record<string, unknown>>({});
  const [payloadValues, setPayloadValues] = createSignal<Record<string, unknown>>({});
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const optsMap = () => (options.latest ?? {}) as OptionsMap;
  const payMap = () => (payloadOptions.latest ?? {}) as OptionsMap;

  const required = () => Object.entries(optsMap()).filter(([, o]) => o?.required).map(([k]) => k);
  const optional = () => Object.entries(optsMap()).filter(([, o]) => o && !o.required).map(([k]) => k);
  const ordered = () => [...required(), ...optional()];

  // resolved() is exactly what launch() sends: defaults, the RHOSTS prefill
  // and the operator's edits. Required-option gating uses the same view.
  function resolved(): Record<string, unknown> {
    const base = defaultsFor(optsMap());
    if (props.prefillHost && !base.RHOSTS) base.RHOSTS = props.prefillHost;
    return { ...base, ...values() };
  }

  function resolvedPayload(): Record<string, unknown> {
    return { ...defaultsFor(payMap()), ...payloadValues() };
  }

  const blockers = () => missingLaunchOptions({
    optionsLoading: options.loading,
    optionsError: !!options.error,
    payloadChosen: !!payload(),
    payloadLoading: payloadOptions.loading,
    optsDefs: optsMap(),
    optsValues: resolved(),
    payDefs: payMap(),
    payValues: resolvedPayload(),
  });

  async function launch() {
    setBusy(true);
    setError("");
    try {
      await ws.command("module.execute", {
        type: props.type, name: props.path,
        options: collectOptions(optsMap(), resolved()),
        ...(payload() ? { payload: payload(), payloadOptions: collectOptions(payMap(), resolvedPayload()) } : {}),
      });
      props.onClose();
    } catch (e) {
      setError(e instanceof CommandError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Launch ${props.path}`} onClose={props.onClose} width="560px">
      <Show when={!info.loading && !info.error && info()} fallback={
        <Show when={info.error} fallback={<p>Loading module…</p>}>
          <p style="color:var(--red-br); margin-top:4px">
            Could not load module info: {String((info.error as Error)?.message ?? info.error)}
          </p>
        </Show>
      }>
        {(i) => <>
          <p style="margin-top:4px; font:400 12.5px var(--sans); color:var(--tx1)">
            {i().name} · rank <b style="color:var(--red-br)">{i().rank}</b>
          </p>
          <p style="margin-top:8px; max-height:84px; overflow:auto; font:400 12px/1.55 var(--sans); color:var(--tx1)">
            {i().description}
          </p>
        </>}
      </Show>

      <Show when={!options.loading && !options.error && options.latest}>
        <div style="margin-top:16px; display:grid; gap:10px; max-height:300px; overflow:auto; padding-right:6px">
          <For each={ordered()}>{(name) =>
            <Show when={optsMap()[name]}>
              {(def) => <OptionField name={name} def={def()}
                value={() => resolved()[name]}
                onEdit={(v) => setValues(prev => ({ ...prev, [name]: v }))} />}
            </Show>
          }</For>
        </div>
      </Show>
      <Show when={options.error}>
        <p style="color:var(--red-br); margin-top:12px">
          Could not load module options: {String((options.error as Error)?.message ?? options.error)}
        </p>
      </Show>

      <Show when={!payloads.loading && (payloads.latest?.length ?? 0) > 0}>
        <label style="margin-top:12px; display:grid; gap:4px">
          <span style="font:500 11px var(--sans); color:var(--tx1)">PAYLOAD</span>
          <select value={payload()} onChange={(e) => { setPayload(e.currentTarget.value); setPayloadValues({}); }}>
            <option value="">(default)</option>
            <For each={payloads.latest ?? []}>{(p) => <option value={p}>{p}</option>}</For>
          </select>
        </label>
        <Show when={payload() && !payloadOptions.loading && Object.keys(payMap()).length > 0}>
          <div style="margin-top:10px; display:grid; gap:10px; max-height:180px; overflow:auto; padding-right:6px">
            <For each={Object.keys(payMap())}>{(name) =>
              <Show when={payMap()[name]}>
                {(def) => <OptionField name={name} def={def()}
                  value={() => resolvedPayload()[name]}
                  onEdit={(v) => setPayloadValues(prev => ({ ...prev, [name]: v }))} />}
              </Show>
            }</For>
          </div>
        </Show>
      </Show>

      <Show when={blockers().length > 0}>
        <p style="color:var(--amb); margin-top:12px">
          Required option{blockers().length > 1 ? "s" : ""} {blockers().join(", ")} need{blockers().length === 1 ? "s" : ""} a value.
        </p>
      </Show>
      <Show when={error()}><p style="color:var(--red-br); margin-top:12px">{error()}</p></Show>

      <div class="mbtns">
        <button class="abtn" style="flex:none; padding:0 20px"
          disabled={launchDisabled({
            busy: busy(),
            optionsLoading: options.loading,
            payloadChosen: !!payload(),
            payloadLoading: payloadOptions.loading,
            missing: blockers(),
          })} onClick={() => void launch()}>
          {busy() ? "Launching…" : "Launch"}
        </button>
      </div>
    </Modal>
  );
}
