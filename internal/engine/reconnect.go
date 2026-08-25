package engine

import (
	"context"
	"errors"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// reconnect runs when the monitor reports persistent RPC failure. Campaign
// data is kept; the engine retries the link with backoff until it returns or
// the operator disconnects.
func (e *Engine) reconnect() {
	e.mu.Lock()
	if e.rpc == nil || e.conn.Status != "connected" || e.connecting {
		e.mu.Unlock()
		return
	}
	gen := e.gen
	cancel := e.runCancel
	// the dropped link owns consoles and a login on the daemon; capture
	// them before the reset so they can be released, not just forgotten
	dropRPC := e.rpc
	dropConsoleID := e.consoleID
	dropRouteCID := ""
	if e.routeConsole != nil {
		dropRouteCID = e.routeConsole.CID
	}
	e.runCancel = nil
	e.rpc = nil
	e.monitor = nil
	e.console = nil
	e.consoleID = ""
	e.routeConsole = nil
	e.interactSID = ""
	e.interactOut = nil
	p := protocol.ConnectParams{
		Host: e.conn.Host, Port: e.conn.Port, SSL: e.conn.SSL, Username: e.conn.Username,
		Password: e.lastPassword,
	}
	e.conn.Status = "reconnecting"
	e.logf(protocol.LevelWarn, "connection lost; reconnecting to %s:%d", p.Host, p.Port)
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// the daemon is likely the reason we are here; release is bounded and
	// async so a stalled one cannot slow the retry loop
	go release(dropRPC, dropConsoleID, dropRouteCID)
	e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))
	e.bus.send(protocol.InteractUpdate(&protocol.InteractState{}))

	backoff := time.Second
	const maxBackoff = 10 * time.Second
	for {
		e.mu.Lock()
		abandoned := e.conn.Status == "disconnected" || e.gen != gen
		e.mu.Unlock()
		if abandoned {
			return // the operator disconnected or a newer connect took over
		}

		err := e.bootstrap(context.Background(), p, gen)
		if err == nil {
			e.mu.Lock()
			if e.gen != gen+1 {
				e.mu.Unlock()
				return // superseded between commit and status flip
			}
			e.conn.Status = "connected"
			e.password = p.Password
			e.errStreak = 0
			e.logf(protocol.LevelSuccess, "reconnected to %s:%d", p.Host, p.Port)
			e.mu.Unlock()
			e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))
			// bootstrap swapped the whole campaign; browsers that stayed
			// attached through the blip need the full state again
			e.bus.send(protocol.NewSnapshot(e.State()))
			return
		}
		if errors.Is(err, errSuperseded) {
			return // a newer connection owns the engine now
		}

		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
