import { For, Show, createEffect, createMemo, createSignal } from "solid-js";

export interface FilterOption {
  value: string;
  group?: string;
}

// Native type-ahead only matches prefixes; this filter matches substrings.
export function FilterSelect(props: {
  options: FilterOption[];
  value: string;
  onChange: (value: string) => void;
  // names the list in the filter placeholder, e.g. "payloads" or "hosts"
  label: string;
  // when set, an empty-value option with this label leads the list (the
  // payload picker's "(default)")
  blankLabel?: string;
}) {
  const [filter, setFilter] = createSignal("");
  let selectEl: HTMLSelectElement | undefined;
  const needle = () => filter().trim().toLowerCase();

  const groups = createMemo(() => {
    const match = needle();
    const out = new Map<string, string[]>();
    let selected = false;
    for (const option of props.options) {
      const hit = !match || option.value.toLowerCase().includes(match);
      if (option.value === props.value) selected = selected || hit;
      if (!hit) continue;
      const bucket = out.get(option.group ?? "") ?? [];
      bucket.push(option.value);
      out.set(option.group ?? "", bucket);
    }
    // the live selection stays visible even when the filter excludes it:
    // a select whose value is missing from its options renders blank
    if (props.value && !selected) {
      const option = props.options.find(o => o.value === props.value);
      const bucket = out.get(option?.group ?? "") ?? [];
      bucket.push(props.value);
      out.set(option?.group ?? "", bucket);
    }
    return [...out.entries()];
  });

  // filtering rebuilds the option elements; a select whose options were
  // replaced shows its first entry even though the value binding is
  // unchanged, so the selection is reapplied after every rebuild
  createEffect(() => {
    groups();
    if (selectEl && selectEl.value !== props.value) selectEl.value = props.value;
  });

  return (
    <div class="fselect">
      <input value={filter()} onInput={(e) => setFilter(e.currentTarget.value)}
        placeholder={`filter ${props.options.length} ${props.label}`}
        aria-label={`Filter ${props.label}`}
        autocomplete="off" spellcheck={false} />
      <select ref={selectEl} value={props.value} aria-label={`Select ${props.label}`}
        onChange={(e) => props.onChange(e.currentTarget.value)}>
        <Show when={props.blankLabel}>
          <option value="">{props.blankLabel}</option>
        </Show>
        <For each={groups()} fallback={<option value="" disabled>no matches</option>}>
          {([group, values]) => (
            <Show when={group} fallback={
              <For each={values}>{(v) => <option value={v}>{v}</option>}</For>
            }>
              <optgroup label={group}>
                <For each={values}>{(v) => <option value={v}>{v}</option>}</For>
              </optgroup>
            </Show>
          )}
        </For>
      </select>
    </div>
  );
}
