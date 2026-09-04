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
			result, err := console.Read(ctx)
			if err != nil {
				e.monitorError(monitor, err)
				continue
			}
			e.consoleRead(console, result)
		}
	}
}

func (e *Engine) consoleRead(console *gomsf.MsfConsole, result *gomsf.ConsoleReadResult) {
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
	if !result.Busy && prompt != "" && !bytes.HasSuffix(e.consoleOut, []byte(prompt)) {
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
	e.mu.Unlock()
}
