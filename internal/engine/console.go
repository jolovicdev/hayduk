package engine

import (
	"bytes"
	"context"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// operatorConsole is one operator's private msf console: the daemon handle,
// the read loop's cancel, and the transcript state browsers mirror.
// writePending marks a written command whose completion no read has
// confirmed yet: readiness is restored to browsers only after a read
// reports the console idle, never straight off the write. Reads and writes
// race on the daemon, so writeGen separates reads issued before a write
// (which describe the old console) from reads issued after it - only the
// latter may acknowledge completion. All fields are guarded by e.mu.
type operatorConsole struct {
	name         string
	console      *gomsf.MsfConsole
	loopCtx      context.Context
	cancel       context.CancelFunc
	out          []byte
	prompt       string
	writePending bool
	writeGen     uint64
}

// consoleGeneration snapshots the write generation for a read about to be
// issued: the read may acknowledge a pending write only when the generation
// still matches, i.e. no write landed after the read was issued.
func (e *Engine) consoleGeneration(oc *operatorConsole) uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return oc.writeGen
}

func (e *Engine) consoleLoop(ctx context.Context, monitor *gomsf.EventMonitor, oc *operatorConsole) {
	ticker := time.NewTicker(e.cfg.OutputInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// the generation is captured before the read is issued: a read
			// that was already in flight when a write landed describes the
			// old console and must not acknowledge the write's completion
			gen := e.consoleGeneration(oc)
			result, err := oc.console.Read(ctx)
			if err != nil {
				e.monitorError(monitor, err)
				continue
			}
			e.consoleRead(oc, result, gen)
		}
	}
}

func (e *Engine) consoleRead(oc *operatorConsole, result *gomsf.ConsoleReadResult, readGen uint64) {
	data := cleanOutput(result.Data)
	prompt := cleanOutput(result.Prompt)

	e.mu.Lock()
	if e.consoles[oc.name] != oc {
		e.mu.Unlock()
		return
	}
	replaced := false
	if oc.prompt != "" && bytes.HasSuffix(oc.out, []byte(oc.prompt)) &&
		(result.Busy || data != "" || prompt != oc.prompt) {
		oc.out = oc.out[:len(oc.out)-len(oc.prompt)]
		replaced = true
	}
	stream := data
	if data != "" {
		oc.out = appendCapped(oc.out, []byte(data))
	}
	// a stale read - issued before a still-pending write - must not append
	// or emit the prompt: every prompt-terminated message reads as ready in
	// browsers, whatever path it takes
	staleRead := oc.writePending && readGen != oc.writeGen
	if !result.Busy && prompt != "" && !staleRead && !bytes.HasSuffix(oc.out, []byte(prompt)) {
		oc.out = appendCapped(oc.out, []byte(prompt))
		stream += prompt
	}
	oc.prompt = prompt
	output := string(oc.out)
	if replaced {
		e.bus.send(protocol.ConsoleUpdate(oc.name, &protocol.ConsoleState{Output: output}))
	} else if stream != "" {
		e.bus.send(protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Operator: oc.name, Data: stream})
	}
	if !result.Busy && oc.writePending && readGen == oc.writeGen {
		// a read issued after the write reports the console idle: browsers
		// that cleared their local prompt on send get the full buffer back.
		// A silent command produces no output and no prompt change, so this
		// is the only path that restores them.
		oc.writePending = false
		e.bus.send(protocol.ConsoleUpdate(oc.name, &protocol.ConsoleState{Output: output}))
	}
	e.mu.Unlock()
}

// consoleFor returns the operator's console, creating it on first use. A
// join while connected already created one; this covers the local operator
// and any race where presence landed mid-bootstrap.
func (e *Engine) consoleFor(ctx context.Context, operator string) (*operatorConsole, *protocol.ErrorBody) {
	e.mu.Lock()
	oc, rpc := e.consoles[operator], e.rpc
	e.mu.Unlock()
	if oc != nil {
		return oc, nil
	}
	if rpc == nil {
		return nil, notConnected()
	}
	if oc = e.ensureConsole(ctx, operator); oc == nil {
		return nil, notConnected()
	}
	return oc, nil
}

// ensureConsole creates the operator's daemon console unless one exists.
// Creation runs outside e.mu (it is an RPC) under consoleMu, so concurrent
// joins for one name produce one console. The read loop runs under the
// link's runCtx and dies with the link or the console's own cancel.
func (e *Engine) ensureConsole(ctx context.Context, name string) *operatorConsole {
	e.mu.Lock()
	if oc := e.consoles[name]; oc != nil {
		e.mu.Unlock()
		return oc
	}
	rpc, runCtx := e.rpc, e.runCtx
	e.mu.Unlock()
	if rpc == nil || runCtx == nil {
		return nil
	}

	e.consoleMu.Lock()
	defer e.consoleMu.Unlock()
	// re-check under consoleMu: another creator may have won, or the link
	// may have been replaced while we waited
	e.mu.Lock()
	if oc := e.consoles[name]; oc != nil {
		e.mu.Unlock()
		return oc
	}
	rpc, runCtx = e.rpc, e.runCtx
	e.mu.Unlock()
	if rpc == nil || runCtx == nil {
		return nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	con, err := gomsf.NewConsoleManager(rpc).Create(cctx)
	if err != nil {
		e.eventfOp(name, protocol.LevelWarn, "console for operator %s unavailable: %v", name, err)
		return nil
	}
	loopCtx, loopCancel := context.WithCancel(runCtx)
	oc := &operatorConsole{name: name, console: gomsf.NewMsfConsole(rpc, con.ID), loopCtx: loopCtx, cancel: loopCancel}

	e.mu.Lock()
	if e.rpc != rpc || e.consoles[name] != nil {
		// the link died or a bootstrap committed while we created: this
		// console must not linger on the daemon
		winner := e.consoles[name]
		e.mu.Unlock()
		loopCancel()
		go releaseConsoles(rpc, []string{con.ID})
		return winner
	}
	if e.consoles == nil {
		e.consoles = make(map[string]*operatorConsole)
	}
	e.consoles[name] = oc
	mon := e.monitor
	e.mu.Unlock()
	go e.consoleLoop(loopCtx, mon, oc)
	return oc
}

// releaseConsoles destroys daemon consoles best effort with a short
// deadline: the daemon may already be gone or hanging, and cleanup must
// never wedge the caller.
func releaseConsoles(rpc gomsf.RPCCaller, ids []string) {
	if rpc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cm := gomsf.NewConsoleManager(rpc)
	for _, id := range ids {
		if id != "" {
			_ = cm.Destroy(ctx, id)
		}
	}
}
