import { For, Show, createEffect, createSignal } from "solid-js";
import { applyCompletion, pushHistory, recallHistory } from "./consoleInput";

export function ConsoleView(props: {
  output: () => string;
  prompt: string;
  busy: boolean;
  write: (cmd: string) => void;
  tabComplete: (line: string) => Promise<string[]>;
}) {
  const [input, setInput] = createSignal("");
  const [history, setHistory] = createSignal<string[]>([]);
  const [histIdx, setHistIdx] = createSignal(-1);
  let scrollEl: HTMLDivElement | undefined;
  let inputEl: HTMLInputElement | undefined;

  const lines = () => props.output().split("\n");

  createEffect(() => {
    props.output();
    if (scrollEl) scrollEl.scrollTop = scrollEl.scrollHeight;
  });

  function submit() {
    if (props.busy) return;
    const cmd = input();
    if (!cmd.trim()) {
      setInput("");
      return;
    }
    props.write(cmd);
    setHistory(h => pushHistory(h, cmd));
    setHistIdx(-1);
    setInput("");
  }

  async function onKey(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
      return;
    }
    if (e.key === "Tab") {
      e.preventDefault();
      const line = input();
      const fragment = line.split(/\s+/).at(-1) ?? "";
      if (!fragment) return;
      try {
        const options = await props.tabComplete(line);
        // the operator may have kept typing while the answer was in
        // flight; applying a completion for stale text clobbers their input
        if (input() !== line) return;
        const completed = applyCompletion(line, options, fragment);
        if (completed !== null) setInput(completed);
      } catch {
        // completion is best-effort
      }
      return;
    }
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      const h = history();
      if (!h.length) return;
      const r = recallHistory(h, histIdx(), e.key === "ArrowUp" ? "up" : "down");
      setHistIdx(r.idx);
      setInput(r.text);
    }
  }

  return (
    <div class="console" ref={scrollEl} onClick={() => inputEl?.focus()}>
      <For each={lines()}>{(l) => <div class="cl out">{l || "\u00a0"}</div>}</For>
      <div class="inputline">
        <span class="pr">{props.prompt}</span>
        <Show when={!props.busy} fallback={<span>framework is busy</span>}>
          <input ref={inputEl} value={input()}
            onInput={(e) => setInput(e.currentTarget.value)}
            onKeyDown={(e) => void onKey(e)}
            placeholder="type a framework command"
            autocomplete="off" spellcheck={false} />
        </Show>
      </div>
    </div>
  );
}
