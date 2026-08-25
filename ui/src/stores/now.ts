import { createSignal, onCleanup, onMount } from "solid-js";

// createNowSignal ticks wall-clock time so ages rendered from it stay live
// instead of freezing at whatever moment the data last changed. Mount it
// inside the component whose lifetime matches the ticking need.
export function createNowSignal(intervalMs = 30_000): () => number {
  const [now, setNow] = createSignal(Date.now());
  onMount(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    onCleanup(() => window.clearInterval(timer));
  });
  return now;
}
