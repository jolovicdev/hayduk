package engine

import (
	"bytes"
	"context"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func (e *Engine) consoleLoop(ctx context.Context, monitor *gomsf.EventMonitor, console *gomsf.MsfConsole) {
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
			gen := e.consoleGeneration()
			result, err := console.Read(ctx)
			if err != nil {
				e.monitorError(monitor, err)
				continue
			}
			e.consoleRead(console, result, gen)
		}
	}
}

func (e *Engine) consoleRead(console *gomsf.MsfConsole, result *gomsf.ConsoleReadResult, readGen uint64) {
	data := cleanOutput(result.Data)
	prompt := cleanOutput(result.Prompt)

	e.mu.Lock()
	if console != e.console {
		e.mu.Unlock()
		return
	}
	replaced := false
	if e.consolePrompt != "" && bytes.HasSuffix(e.consoleOut, []byte(e.consolePrompt)) &&
		(result.Busy || data != "" || prompt != e.consolePrompt) {
		e.consoleOut = e.consoleOut[:len(e.consoleOut)-len(e.consolePrompt)]
		replaced = true
	}
	stream := data
	if data != "" {
		e.consoleOut = appendCapped(e.consoleOut, []byte(data))
	}
	// a stale read - issued before a still-pending write - must not append
	// or emit the prompt: every prompt-terminated message reads as ready in
	// browsers, whatever path it takes
	staleRead := e.consoleWritePending && readGen != e.consoleWriteGen
	if !result.Busy && prompt != "" && !staleRead && !bytes.HasSuffix(e.consoleOut, []byte(prompt)) {
		e.consoleOut = appendCapped(e.consoleOut, []byte(prompt))
		stream += prompt
	}
	e.consolePrompt = prompt
	output := string(e.consoleOut)
	if replaced {
		e.bus.send(protocol.ConsoleUpdate(&protocol.ConsoleState{Output: output}))
	} else if stream != "" {
		e.bus.send(protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Data: stream})
	}
	if !result.Busy && e.consoleWritePending && readGen == e.consoleWriteGen {
		// a read issued after the write reports the console idle: browsers
		// that cleared their local prompt on send get the full buffer back.
		// A silent command produces no output and no prompt change, so this
		// is the only path that restores them.
		e.consoleWritePending = false
		e.bus.send(protocol.ConsoleUpdate(&protocol.ConsoleState{Output: output}))
	}
	e.mu.Unlock()
}
