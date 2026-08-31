// msf option values must keep their types: rpc_execute rejects "443" where
// it wants an integer and "true" where it wants a boolean.

export interface OptionDef {
  type: string;
  required: boolean;
  enums?: string[];
  default?: unknown;
}

export type OptionKind = "text" | "number" | "bool" | "enum";

export function optionKind(o: OptionDef): OptionKind {
  if ((o.enums?.length ?? 0) > 0) return "enum";
  switch (o.type) {
    case "bool":
    case "boolean":
      return "bool";
    case "integer":
    case "int":
    case "port":
    case "numeric":
      return "number";
    default:
      return "text";
  }
}

export function defaultText(v: unknown): string {
  return v === undefined || v === null ? "" : String(v);
}

function numberValue(text: string): number | undefined {
  const n = Number(text);
  return Number.isFinite(n) ? n : undefined;
}

// collectOptions turns edit-state strings into typed values, dropping empty
// and unparseable entries.
export function collectOptions(
  defs: Record<string, OptionDef>,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, def] of Object.entries(defs)) {
    const kind = optionKind(def);
    const raw = values[name];
    if (kind === "bool") {
      if (typeof raw === "boolean") out[name] = raw;
      else if (raw === "true") out[name] = true;
      else if (raw === "false") out[name] = false;
      continue;
    }
    if (typeof raw !== "string") continue;
    const text = raw.trim();
    if (!text) continue;
    if (kind === "number") {
      const n = numberValue(text);
      if (n !== undefined) out[name] = n;
      continue;
    }
    out[name] = text;
  }
  return out;
}

// missingRequired names required options that resolve to no usable value
// (empty text, or a numeric field holding garbage). Defaults count.
export function missingRequired(
  defs: Record<string, OptionDef>,
  values: Record<string, unknown>,
): string[] {
  const missing: string[] = [];
  for (const [name, def] of Object.entries(defs)) {
    if (!def.required) continue;
    const raw = values[name];
    const hasDefault = def.default !== undefined && def.default !== null && def.default !== "";
    if (optionKind(def) === "bool") {
      if (typeof raw === "boolean" || raw === "true" || raw === "false" || hasDefault) continue;
      missing.push(name);
      continue;
    }
    const text = typeof raw === "string" ? raw.trim() : "";
    if (text) {
      if (optionKind(def) === "number" && numberValue(text) === undefined) missing.push(name);
      continue;
    }
    if (!hasDefault) missing.push(name);
  }
  return missing;
}

// The compatible-payloads request must identify the module the way every
// other module command does.
export function compatiblePayloadsParams(
  type: string,
  name: string,
): Record<string, string> | null {
  if (type !== "exploit") return null;
  return { type, name };
}

// missingLaunchOptions names the required options blocking a launch: the
// module's own, plus the chosen payload's once its settings are in hand.
export function missingLaunchOptions(o: {
  optionsLoading: boolean;
  optionsError: boolean;
  payloadChosen: boolean;
  payloadLoading: boolean;
  optsDefs: Record<string, OptionDef>;
  optsValues: Record<string, unknown>;
  payDefs: Record<string, OptionDef>;
  payValues: Record<string, unknown>;
}): string[] {
  if (o.optionsLoading || o.optionsError) return [];
  const missing = missingRequired(o.optsDefs, o.optsValues);
  if (o.payloadChosen && !o.payloadLoading) {
    missing.push(...missingRequired(o.payDefs, o.payValues));
  }
  return missing;
}

// The launch button's gate: no launch while either option set is still
// loading (it would send without defaults), while the module's options failed
// to load (it would send an empty option set), or while anything is missing.
export function launchDisabled(o: {
  busy: boolean;
  optionsLoading: boolean;
  optionsError: boolean;
  payloadChosen: boolean;
  payloadLoading: boolean;
  missing: string[];
}): boolean {
  if (o.busy || o.optionsLoading || o.optionsError) return true;
  if (o.payloadChosen && o.payloadLoading) return true;
  return o.missing.length > 0;
}
