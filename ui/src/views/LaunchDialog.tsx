import { For, Show, createResource, createSignal } from "solid-js";
import { Modal } from "../components/modal";
import { FilterSelect } from "../components/FilterSelect";
import { ws } from "../ws/singleton";
import { CommandError } from "../ws/client";
import {
  collectOptions, compatiblePayloadsParams, defaultText, launchDisabled, missingLaunchOptions, optionKind, payloadGroup,
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
  if (kind() === "bool") {
    return (
      <label class="checkline">
        <input type="checkbox" checked={props.value() === true || props.value() === "true"}
          onChange={(e) => props.onEdit(e.currentTarget.checked)} />
        <span>{props.name}
          <Show when={props.def.required}><span class="req"> *</span></Show>
          <span class="fdesc"> ({props.def.desc})</span>
        </span>
      </label>
    );
  }
  return (
    <label class="field">
      <b>
        {props.name}
        <Show when={props.def.required}><span class="req"> *</span></Show>
        <span class="fdesc"> ({props.def.desc})</span>
      </b>
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
    </label>
  );
}

export function LaunchDialog(props: {
  type: string;
  path: string;
  prefillHost?: string;
  // prefillPort is the service port a Find-attacks match was struck on; it
  // seeds RPORT so the exploit points at the port that matched, not the
  // module default. It stays visible and editable as a normal option.
  prefillPort?: number;
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
  // TARGET is an execution option, not a registered module option, so it does
  // not appear in module.options. module.info carries the target list; the
  // selected index rides the execute options where msf expects it.
  const [target, setTarget] = createSignal("0");
  const [payloadOptions] = createResource(payload, async (p) => {
    if (!p) return null;
    return ws.command<OptionsMap>("module.options", { type: "payload", name: p });
  });
  const [values, setValues] = createSignal<Record<string, unknown>>({});
  const [payloadValues, setPayloadValues] = createSignal<Record<string, unknown>>({});
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const optsMap = () => (options.latest ?? {}) as OptionsMap;
  const payMap = () => (payloadOptions.latest ?? {}) as OptionsMap;
  const targets = () => info.latest?.targets ?? [];

  const required = () => Object.entries(optsMap()).filter(([, o]) => o?.required).map(([k]) => k);
  const optional = () => Object.entries(optsMap()).filter(([, o]) => o && !o.required).map(([k]) => k);
  const ordered = () => [...required(), ...optional()];

  // resolved() is exactly what launch() sends: defaults, the RHOSTS prefill
  // and the operator's edits. Required-option gating uses the same view.
  function resolved(): Record<string, unknown> {
    const base = defaultsFor(optsMap());
    if (props.prefillHost && !base.RHOSTS) base.RHOSTS = props.prefillHost;
    if (props.prefillPort && optsMap().RPORT) base.RPORT = String(props.prefillPort);
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
      const runOptions = collectOptions(optsMap(), resolved());
      if (targets().length > 1) runOptions.TARGET = Number(target());
      await ws.command("module.execute", {
        type: props.type, name: props.path,
        options: runOptions,
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
        <Show when={info.error} fallback={
          <div class="opt-skeleton tight" aria-hidden="true">
            <div class="skel"></div>
            <div class="skel short"></div>
          </div>
        }>
          <p class="merror">
            Could not load module info: {String((info.error as Error)?.message ?? info.error)}
          </p>
        </Show>
      }>
        {(i) => <>
          <p style="margin-top:4px">
            {i().name} · rank <b class="req">{i().rank}</b>
          </p>
          <p class="mtext" style="margin-top:8px; max-height:84px; overflow:auto">
            {i().description}
          </p>
        </>}
      </Show>

      <Show when={targets().length > 1}>
        <label class="field" style="margin-top:12px">
          <b>Target <span class="fdesc">(exploit target, not an option)</span></b>
          <select value={target()} onChange={(e) => setTarget(e.currentTarget.value)}>
            <For each={targets()}>{(t, i) => <option value={String(i())}>{i()} · {t}</option>}</For>
          </select>
        </label>
      </Show>

      <Show when={!options.loading && !options.error && options.latest} fallback={
        // Reserve space to keep the dialog height stable while options load.
        <Show when={options.loading}>
          <div class="opt-skeleton" aria-hidden="true">
            <div class="skel"></div>
            <div class="skel"></div>
            <div class="skel short"></div>
          </div>
        </Show>
      }>
        <div class="optlist">
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
        <p class="merror">
          Could not load module options: {String((options.error as Error)?.message ?? options.error)}
        </p>
      </Show>
      <Show when={payload() && payloadOptions.error}>
        <p class="merror">
          Could not load payload options: {String((payloadOptions.error as Error)?.message ?? payloadOptions.error)}
        </p>
      </Show>

      <Show when={payloads.loading}>
        <div class="opt-skeleton tight" aria-hidden="true">
          <div class="skel"></div>
          <div class="skel short"></div>
        </div>
      </Show>
      <Show when={!payloads.loading && (payloads.latest?.length ?? 0) > 0}>
        <div class="field" style="margin-top:12px">
          <b>Payload</b>
          <FilterSelect
            options={(payloads.latest ?? []).map(p => ({ value: p, group: payloadGroup(p) }))}
            value={payload()} label="payloads" blankLabel="(default)"
            onChange={(p) => { setPayload(p); setPayloadValues({}); }} />
        </div>
        <Show when={payload() && payloadOptions.loading}>
          <div class="opt-skeleton tight" aria-hidden="true">
            <div class="skel"></div>
          </div>
        </Show>
        <Show when={payload() && !payloadOptions.loading && Object.keys(payMap()).length > 0}>
          <div class="optlist tight">
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
        <p class="mwarn">
          Required option{blockers().length > 1 ? "s" : ""} {blockers().join(", ")} need{blockers().length === 1 ? "s" : ""} a value.
        </p>
      </Show>
      <Show when={error()}><p class="merror">{error()}</p></Show>

      <div class="mbtns">
        <button class="abtn"
          disabled={launchDisabled({
            busy: busy(),
            optionsLoading: options.loading,
            optionsError: !!options.error,
            payloadChosen: !!payload(),
            payloadLoading: payloadOptions.loading,
            payloadError: !!payloadOptions.error,
            missing: blockers(),
          })} onClick={() => void launch()}>
          {busy() ? "Launching…" : "Launch"}
        </button>
      </div>
    </Modal>
  );
}
