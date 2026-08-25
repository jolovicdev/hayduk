type Handler = (msg: any) => void;

export interface WSClient {
  status(): "connecting" | "open" | "closed";
  command<T = unknown>(method: string, params?: unknown): Promise<T>;
  on(type: string, h: Handler): () => void;
}

export class CommandError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

// must match protocol.ProtoVersion on the Go side
const PROTO_VERSION = 1;

export function wsURL(loc: { protocol: string; host: string } = location): string {
  return `${loc.protocol === "https:" ? "wss" : "ws"}://${loc.host}/ws`;
}

// operatorHolder names the acting operator (team mode); every command is
// stamped with it.
const operatorHolder = { value: "" };

export function setOperator(name: string) {
  operatorHolder.value = name;
}

// currentOperator exposes the live identity (header chip, tests).
export function currentOperator(): string {
  return operatorHolder.value;
}

export function createWS(): WSClient {
  let sock: WebSocket | null = null;
  let nextId = 1;
  let backoff = 500;
  const pending = new Map<number, { resolve: (v: any) => void; reject: (e: any) => void }>();
  const handlers = new Map<string, Set<Handler>>();
  const state = { status: "connecting" as "connecting" | "open" | "closed" };

  function connect() {
    state.status = "connecting";
    sock = new WebSocket(wsURL());
    // a mismatched server must not be reconnected against forever
    let fatal = false;
    sock.addEventListener("open", () => {
      state.status = "open";
      backoff = 500;
      handlers.get("open")?.forEach(h => h({}));
    });
    sock.addEventListener("message", (ev) => {
      let msg: any;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        console.warn("unparseable ws message", ev.data);
        return;
      }
      if (msg?.type === "hello") {
        if (typeof msg.proto === "number" && msg.proto !== PROTO_VERSION) {
          fatal = true;
          handlers.get("proto_mismatch")?.forEach(h => h({ proto: msg.proto, version: msg.version }));
          try { sock?.close(); } catch { /* already closing */ }
          return;
        }
        // version and team mode are live values, re-read on every hello
        handlers.get("hello")?.forEach(h => h(msg));
        return;
      }
      if (msg?.type === "response") {
        const p = pending.get(msg.id);
        pending.delete(msg.id);
        if (!p) return;
        if (msg.ok) p.resolve(msg.data);
        else p.reject(new CommandError(msg.error?.code ?? "internal", msg.error?.message ?? ""));
        return;
      }
      if (msg?.type) {
        handlers.get(msg.type)?.forEach(h => h(msg));
      } else {
        console.warn("ws message without type", msg);
      }
    });
    sock.addEventListener("close", () => {
      state.status = "closed";
      const err = new CommandError("disconnected", "connection to hayduk lost");
      for (const p of pending.values()) {
        p.reject(err);
      }
      pending.clear();
      handlers.get("closed")?.forEach(h => h({}));
      if (!fatal) {
        setTimeout(connect, backoff);
        backoff = Math.min(backoff * 2, 8000);
      }
    });
  }
  connect();

  return {
    status: () => state.status,
    command<T>(method: string, params?: unknown): Promise<T> {
      const id = nextId++;
      return new Promise<T>((resolve, reject) => {
        // sending on a half-open socket throws or vanishes; fail fast so
        // callers see a rejection instead of a promise that never settles
        if (!sock || sock.readyState !== 1) {
          reject(new CommandError("disconnected", "connection to hayduk is not open"));
          return;
        }
        pending.set(id, { resolve, reject });
        try {
          sock.send(JSON.stringify({
            type: "command", id, method, params,
            ...(operatorHolder.value ? { operator: operatorHolder.value } : {}),
          }));
        } catch (err: any) {
          pending.delete(id);
          reject(new CommandError("disconnected", `send failed: ${err?.message ?? err}`));
        }
      });
    },
    on(type: string, h: Handler) {
      let set = handlers.get(type);
      if (!set) {
        set = new Set();
        handlers.set(type, set);
      }
      set.add(h);
      return () => set.delete(h);
    },
  };
}
