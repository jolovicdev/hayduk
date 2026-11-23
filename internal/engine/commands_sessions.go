package engine

import (
	"bytes"
	"context"
	"strings"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// attach opens one operator's view of a session. Transcripts are shared:
// the first attach anywhere starts a fresh one, and every later attach -
// by any operator - replays the buffered history. The watch is refcounted
// across operators, so one detach cannot silence another's live stream.
func (e *Engine) attach(operator, sid string) *protocol.ErrorBody {
	e.mu.Lock()
	if e.sessions[sid] == nil {
		e.mu.Unlock()
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + sid}
	}
	if e.interactBufs == nil {
		e.interactBufs = make(map[string][]byte)
	}
	if _, ok := e.interactBufs[sid]; !ok {
		// first attach starts a fresh transcript; re-attaching keeps the
		// buffered one, which is already consumed from the RPC stream and
		// cannot be polled back
		e.interactBufs[sid] = nil
	}
	if e.attached == nil {
		e.attached = make(map[string]map[string]bool)
	}
	if e.attached[operator] == nil {
		e.attached[operator] = make(map[string]bool)
	}
	firstHold := !e.sessionHeldLocked(sid)
	e.attached[operator][sid] = true
	if e.interactActive == nil {
		e.interactActive = make(map[string]string)
	}
	e.interactActive[operator] = sid
	mon := e.monitor
	// the update carries the buffered transcript: browsers replace their
	// local copy on every interact update and would blank it without this
	e.bus.send(protocol.InteractUpdate(operator, &protocol.InteractState{
		SID: sid, Output: string(e.interactBufs[sid]),
	}))
	e.mu.Unlock()
	if mon != nil && firstHold {
		mon.WatchSession(sid)
	}
	return nil
}

// detach closes one operator's session console, or their active one when
// sid is empty. Their other tabs keep their transcripts, and another
// operator's view of the same session keeps its transcript and stream.
func (e *Engine) detach(operator, sid string) {
	e.mu.Lock()
	if sid == "" {
		sid = e.interactActive[operator]
	}
	if sid == "" || !e.attached[operator][sid] {
		e.mu.Unlock()
		return
	}
	delete(e.attached[operator], sid)
	lastHold := !e.sessionHeldLocked(sid)
	if lastHold {
		delete(e.interactBufs, sid)
	}
	wasActive := e.interactActive[operator] == sid
	if wasActive {
		delete(e.interactActive, operator)
	}
	mon := e.monitor
	if wasActive {
		e.bus.send(protocol.InteractUpdate(operator, &protocol.InteractState{}))
	}
	e.mu.Unlock()
	if mon != nil && lastHold {
		mon.UnwatchSession(sid)
	}
}

func (e *Engine) sessionWrite(ctx context.Context, p protocol.SessionWriteParams) *protocol.ErrorBody {
	e.mu.Lock()
	rpc := e.rpc
	_, attached := e.interactBufs[p.SID]
	session := e.sessions[p.SID]
	e.mu.Unlock()
	if rpc == nil {
		return notConnected()
	}
	if session == nil {
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + p.SID}
	}
	if !attached {
		return &protocol.ErrorBody{Code: protocol.CodeBusy, Message: "session " + p.SID + " is not attached; attach first"}
	}
	data := strings.TrimSuffix(p.Data, "\n") + "\n"
	prompt := p.SID + " sh > "
	if session.Type == "meterpreter" {
		prompt = "meterpreter " + p.SID + " > "
	}
	echo := prompt + strings.TrimSuffix(p.Data, "\n") + "\n"
	// Session streams do not echo commands. Store the echo in the engine
	// transcript for re-attach and other operators, using the browser's
	// prompt format. The echo must precede the write: the monitor can
	// deliver the command's output while the write is still in flight.
	// A write that fails rolls it back, because the transcript is what a
	// later re-attach replays and a command that never reached the daemon
	// must not become part of the session's history.
	var echoed bool
	e.mu.Lock()
	if _, ok := e.interactBufs[p.SID]; ok {
		e.interactBufs[p.SID] = appendCapped(e.interactBufs[p.SID], []byte(echo))
		echoed = true
		e.bus.send(protocol.SessionOutputMsg{Type: protocol.KindSessionOutput, SID: p.SID, Data: echo})
	}
	e.mu.Unlock()
	var err error
	if session.Type == "meterpreter" {
		err = gomsf.NewMeterpreterSession(rpc, p.SID).Write(ctx, data)
	} else {
		err = gomsf.NewShellSession(rpc, p.SID).Write(ctx, data)
	}
	if err != nil {
		if echoed {
			e.rollbackEcho(p.SID, echo)
		}
		e.logf(protocol.LevelWarn, "session %s write failed: %v", p.SID, err)
		return mapErr(err)
	}
	return nil
}

// rollbackEcho drops the echo of a write that failed from the transcript.
// The line has already gone out on the bus and cannot be recalled without
// either a new protocol message or hijacking every operator's active tab,
// so the failure is reported in the event log and the session history that
// survives a re-attach stays truthful.
func (e *Engine) rollbackEcho(sid, echo string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if buf, ok := e.interactBufs[sid]; ok {
		if trimmed, cut := bytes.CutSuffix(buf, []byte(echo)); cut {
			e.interactBufs[sid] = trimmed
		}
	}
}

func (e *Engine) sessionUpgrade(ctx context.Context, operator, ws string, p protocol.SessionUpgradeParams) *protocol.ErrorBody {
	e.mu.Lock()
	rpc := e.rpc
	session := e.sessions[p.SID]
	e.mu.Unlock()
	if rpc == nil {
		return notConnected()
	}
	if session == nil {
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + p.SID}
	}
	if session.Type != "shell" {
		return &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "session " + p.SID + " is " + session.Type + ", only shell sessions upgrade"}
	}
	if err := gomsf.NewShellSession(rpc, p.SID).Upgrade(ctx, p.LHOST, p.LPORT); err != nil {
		return mapErr(err)
	}
	e.eventfOpIn(ws, operator, protocol.LevelSuccess, "session %s upgrading to meterpreter via %s:%d", p.SID, p.LHOST, p.LPORT)
	return nil
}
