import { createSignal } from "solid-js";
import { ws } from "../ws/singleton";

const [sid, setSid] = createSignal("");
const [output, setOutput] = createSignal("");
const CAP = 128_000;

ws.on("sessionOutput", (m) => {
  if (m.sid !== sid()) return;
  appendChunk(m.data);
});
ws.on("resource", (m) => {
  if (m.resource === "interact") {
    setSid(m.interact?.sid ?? "");
    setOutput(m.interact?.output ?? "");
  }
});
ws.on("snapshot", (m) => {
  setSid(m.state?.interact?.sid ?? "");
  setOutput(m.state?.interact?.output ?? "");
});

function appendChunk(data: string) {
  setOutput(prev => {
    const next = prev + data;
    return next.length > CAP ? next.slice(next.length - CAP) : next;
  });
}

export function attach(sessionId: string) {
  return ws.command("session.attach", { sid: sessionId });
}

export function detach() {
  return ws.command("session.detach");
}

export function write(data: string) {
  return ws.command("session.write", { sid: sid(), data });
}

export { sid as interactSID, output as interactOutput };
