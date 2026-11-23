package engine

import (
	"sort"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// OperatorJoin and OperatorLeave track connected team operators. The first socket
// under a name gets a private msf console, and the last one's departure
// schedules its console and session attachments for release after a linger.
func (e *Engine) OperatorJoin(name string)  { e.operatorDelta(name, 1) }
func (e *Engine) OperatorLeave(name string) { e.operatorDelta(name, -1) }

func (e *Engine) operatorDelta(name string, delta int) {
	if name == "" {
		return
	}
	e.mu.Lock()
	if e.operators == nil {
		e.operators = make(map[string]int)
	}
	next := e.operators[name] + delta
	changed := false
	becamePresent, becameAbsent := false, false
	switch {
	case next <= 0 && e.operators[name] > 0:
		delete(e.operators, name)
		e.logf(protocol.LevelInfo, "operator %s left", name)
		changed = true
		becameAbsent = true
	case next > 0 && e.operators[name] == 0:
		e.operators[name] = next
		e.logf(protocol.LevelInfo, "operator %s joined", name)
		changed = true
		becamePresent = true
	default:
		if next > 0 {
			e.operators[name] = next
		}
	}
	if becamePresent {
		// a rejoin inside the linger resumes the kept console
		if t := e.consoleTimers[name]; t != nil {
			t.Stop()
			delete(e.consoleTimers, name)
		}
	}
	list := e.operatorListLocked()
	if changed {
		e.bus.send(protocol.OperatorsUpdate(list))
	}
	rpc, runCtx := e.rpc, e.runCtx
	e.mu.Unlock()
	if becamePresent && rpc != nil {
		e.ensureConsole(runCtx, name)
	}
	if becameAbsent {
		e.scheduleConsoleDrop(name)
	}
}

// scheduleConsoleDrop arms the linger timer for a departed operator. The
// timer identity is recorded so a rejoin-and-leave cycle cannot have an
// older timer drop the newer stay.
func (e *Engine) scheduleConsoleDrop(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.operators[name] > 0 {
		return // already rejoined
	}
	var timer *time.Timer
	timer = time.AfterFunc(e.cfg.ConsoleLinger, func() { e.dropOperatorConsole(name, timer) })
	if e.consoleTimers == nil {
		e.consoleTimers = make(map[string]*time.Timer)
	}
	if old := e.consoleTimers[name]; old != nil {
		old.Stop()
	}
	e.consoleTimers[name] = timer
}

// dropOperatorConsole releases a departed operator's console and session
// attachments once the linger expired. Shared transcripts and watches
// survive while another operator still holds the session.
func (e *Engine) dropOperatorConsole(name string, timer *time.Timer) {
	e.mu.Lock()
	if e.consoleTimers[name] != timer || e.operators[name] > 0 {
		e.mu.Unlock()
		return
	}
	delete(e.consoleTimers, name)
	oc := e.consoles[name]
	delete(e.consoles, name)
	cid := ""
	if oc != nil {
		oc.cancel()
		cid = oc.console.CID
	}
	unwatch := e.detachOperatorLocked(name)
	rpc, mon := e.rpc, e.monitor
	e.mu.Unlock()
	if cid != "" && rpc != nil {
		go releaseConsoles(rpc, []string{cid})
	}
	if mon != nil {
		for _, sid := range unwatch {
			mon.UnwatchSession(sid)
		}
	}
}

// detachOperatorLocked removes every session attachment of one operator
// and returns the sessions nobody holds anymore (their transcripts were
// dropped and their watches must end). Callers hold e.mu.
func (e *Engine) detachOperatorLocked(name string) []string {
	var unwatch []string
	for sid := range e.attached[name] {
		if !e.sessionHeldByOtherLocked(name, sid) {
			delete(e.interactBufs, sid)
			unwatch = append(unwatch, sid)
		}
	}
	delete(e.attached, name)
	delete(e.interactActive, name)
	return unwatch
}

// sessionHeldByOtherLocked reports whether any operator other than name
// still holds sid open. Callers hold e.mu.
func (e *Engine) sessionHeldByOtherLocked(name, sid string) bool {
	for op, sids := range e.attached {
		if op != name && sids[sid] {
			return true
		}
	}
	return false
}

// sessionHeldLocked reports whether any operator holds sid open.
func (e *Engine) sessionHeldLocked(sid string) bool {
	for _, sids := range e.attached {
		if sids[sid] {
			return true
		}
	}
	return false
}

func (e *Engine) operatorListLocked() []string {
	out := make([]string, 0, len(e.operators))
	for name := range e.operators {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
