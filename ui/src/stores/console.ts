import { createMemo, createRoot, createSignal } from "solid-js";
import { parsePrompt } from "../components/console";
import { ws } from "../ws/singleton";

const [rawOutput, setRawOutput] = createSignal("");
const CAP = 128_000;
let lastPrompt = "msf > ";

ws.on("consoleOutput", (m) => append(m.data));
ws.on("snapshot", (m) => replace(m.state?.console?.output ?? ""));
ws.on("resource", (m) => {
  if (m.resource === "console" && m.console) replace(m.console.output);
});

const frame = createRoot(() => createMemo(() => {
  const raw = rawOutput();
  const prompt = parsePrompt(raw);
  return {
    output: prompt ? raw.slice(0, prompt.start) : raw,
    prompt: prompt?.value ?? lastPrompt,
    busy: prompt === null,
  };
}));

function replace(output: string) {
  const prompt = parsePrompt(output);
  if (prompt) lastPrompt = prompt.value;
  setRawOutput(output);
}

export function append(chunk: string) {
  setRawOutput(prev => {
    const next = prev + chunk;
    const capped = next.length > CAP ? next.slice(next.length - CAP) : next;
    const prompt = parsePrompt(capped);
    if (prompt) lastPrompt = prompt.value;
    return capped;
  });
}

export function write(command: string) {
  const current = rawOutput();
  const prompt = parsePrompt(current);
  const pending = prompt ? current.slice(0, prompt.start) : current;
  if (prompt) setRawOutput(pending);
  return ws.command("console.write", { command: command + "\n" }).catch(error => {
    if (prompt) setRawOutput(prev => prev === pending ? prev + prompt.value : prev);
    throw error;
  });
}

export function tabs(line: string) {
  return ws.command<string[]>("console.tabs", { line });
}

export const consoleOutput = () => frame().output;
export const consolePrompt = () => frame().prompt;
export const consoleBusy = () => frame().busy;
