import { createSignal, onCleanup } from "solid-js";

// Draggable pane splitter with requestAnimationFrame delta batching so
// high-polling-rate mice never trigger redundant layout passes mid-frame.
export function Splitter(props: {
  area: "lsp" | "rsp" | "nsp";
  onDelta: (dx: number, dy: number) => void;
  onEnd?: () => void;
}) {
  let el!: HTMLDivElement;
  const [active, setActive] = createSignal(false);
  let last: { x: number; y: number } | null = null;
  let accX = 0;
  let accY = 0;
  let frame = 0;

  function flush() {
    frame = 0;
    if (accX === 0 && accY === 0) return;
    const dx = accX;
    const dy = accY;
    accX = 0;
    accY = 0;
    props.onDelta(dx, dy);
  }

  onCleanup(() => {
    if (frame) cancelAnimationFrame(frame);
  });

  function endDrag() {
    if (frame) {
      cancelAnimationFrame(frame);
      flush();
    }
    last = null;
    setActive(false);
    props.onEnd?.();
  }

  return (
    <div
      ref={el}
      class={`splitter ${props.area}`}
      classList={{ active: active() }}
      onPointerDown={(e) => {
        last = { x: e.clientX, y: e.clientY };
        el.setPointerCapture(e.pointerId);
        setActive(true);
      }}
      onPointerMove={(e) => {
        if (!last) return;
        accX += e.clientX - last.x;
        accY += e.clientY - last.y;
        last = { x: e.clientX, y: e.clientY };
        if (!frame) frame = requestAnimationFrame(flush);
      }}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    />
  );
}
