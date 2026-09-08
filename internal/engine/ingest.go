package engine

import (
	"context"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// ingest drains the EventMonitor channel into engine state. It exits when the
// monitor's channel closes (runCtx cancelled). Events from a superseded
// monitor are dropped: its buffered errors must not trip the reconnect logic
// of the monitor that replaced it, and its buffered session events must not
// touch the replacement link's reused session IDs.
func (e *Engine) ingest(m *gomsf.EventMonitor, ch <-chan gomsf.Event) {
	for ev := range ch {
		switch ev.Type {
		case gomsf.EventSessionOpened:
			e.sessionOpened(m, ev)
		case gomsf.EventSessionClosed:
			e.sessionClosed(m, ev)
		case gomsf.EventSessionOutput:
			e.sessionOutput(m, ev)
		case gomsf.EventJobStarted:
			e.jobChanged(m, ev.Job.ID, ev.Job.Name, true)
		case gomsf.EventJobStopped:
			e.jobChanged(m, ev.Job.ID, ev.Job.Name, false)
		case gomsf.EventError:
			e.monitorError(m, ev.Err)
		}
	}
}

func (e *Engine) sessionOpened(m *gomsf.EventMonitor, ev gomsf.Event) {
	e.mu.Lock()
	if m != e.monitor {
		e.mu.Unlock()
		return
	}
	if _, exists := e.sessions[ev.SessionID]; exists {
		e.mu.Unlock()
		return
	}
	// a session the monitor reports new belongs to the campaign that
	// opened it; later workspace switches must not re-attribute it.
	// Sessions seeded by bootstrap (existing at connect) never take this
	// path, so they keep their original - or no - attribution.
	st := sessionState(ev.SessionID, ev.Session, e.conn.Workspace)
	if e.sessionTags == nil {
		e.sessionTags = make(map[string]sessionTag)
	}
	e.sessionTags[ev.SessionID] = sessionTag{workspace: st.Workspace, uuid: st.UUID}
	e.sessions[ev.SessionID] = st
	sessions := copyMap(e.sessions)
	host := hostLabel(st.TargetHost)
	e.logf(protocol.LevelSuccess, "session %s opened (%s) on %s via %s",
		ev.SessionID, st.Type, host, st.ViaExploit)
	e.bus.send(protocol.SessionsUpdate(sessions))
	e.mu.Unlock()
}

// sessionState maps a daemon session to engine state; ws is the workspace
// the session was opened under, empty when unknown.
func sessionState(id string, s *gomsf.Session, ws string) *protocol.SessionState {
	st := &protocol.SessionState{
		ID:        id,
		OpenedAt:  time.Now().UTC(),
		Workspace: ws,
	}
	if s != nil {
		st.Type = s.Type
		st.TunnelPeer = s.TunnelPeer
		st.ViaExploit = s.ViaExploit
		st.ViaPayload = s.ViaPayload
		st.Info = s.Info
		st.Username = s.Username
		st.TargetHost = s.TargetHost
		st.SessionHost = s.SessionHost
		st.UUID = s.UUID
	}
	return st
}

func (e *Engine) sessionClosed(m *gomsf.EventMonitor, ev gomsf.Event) {
	e.mu.Lock()
	if m != e.monitor {
		e.mu.Unlock()
		return
	}
	if _, exists := e.sessions[ev.SessionID]; !exists {
		e.mu.Unlock()
		return
	}
	e.removeSessionLocked(ev.SessionID)
	sessions := copyMap(e.sessions)
	e.logf(protocol.LevelWarn, "session %s closed", ev.SessionID)
	if e.interactSID == ev.SessionID {
		e.interactSID = ""
		e.interactOut = nil
		e.bus.send(protocol.InteractUpdate(&protocol.InteractState{}))
	}
	e.bus.send(protocol.SessionsUpdate(sessions))
	e.mu.Unlock()
}

// reconcileSessions drops sessions the daemon no longer lists. The monitor
// reports closes only for sessions it observed itself, so a session seeded
// by bootstrap that dies before the monitor's first poll would stay visible
// forever. Additions stay the monitor's job: its open events carry
// attribution.
func (e *Engine) reconcileSessions(ctx context.Context) {
	rpc := e.connectedRPC()
	if rpc == nil {
		return
	}
	// a session opened after this instant may be absent from the snapshot
	// legitimately and is left alone this round
	snapshotAt := time.Now()
	live, err := gomsf.NewSessionManager(rpc).List(ctx)
	if err != nil {
		return
	}
	e.mu.Lock()
	if e.rpc != rpc {
		e.mu.Unlock()
		return
	}
	changed := false
	for sid, st := range e.sessions {
		if _, listed := live[sid]; !listed && !st.OpenedAt.After(snapshotAt) {
			e.removeSessionLocked(sid)
			e.logf(protocol.LevelWarn, "session %s closed", sid)
			changed = true
		}
	}
	if changed {
		e.bus.send(protocol.SessionsUpdate(copyMap(e.sessions)))
	}
	e.mu.Unlock()
}

// removeSessionLocked drops a session and its attribution. Callers hold e.mu.
func (e *Engine) removeSessionLocked(sid string) {
	delete(e.sessions, sid)
	delete(e.sessionTags, sid)
}

func (e *Engine) sessionOutput(m *gomsf.EventMonitor, ev gomsf.Event) {
	e.mu.Lock()
	if m != e.monitor {
		e.mu.Unlock()
		return
	}
	if ev.SessionID != e.interactSID {
		e.mu.Unlock()
		return
	}
	data := cleanOutput(ev.Data)
	if data == "" {
		e.mu.Unlock()
		return
	}
	e.interactOut = appendCapped(e.interactOut, []byte(data))
	e.bus.send(protocol.SessionOutputMsg{Type: protocol.KindSessionOutput, SID: ev.SessionID, Data: data})
	e.mu.Unlock()
}

func (e *Engine) jobChanged(m *gomsf.EventMonitor, id, name string, started bool) {
	e.mu.Lock()
	if m != e.monitor {
		e.mu.Unlock()
		return
	}
	if started {
		e.jobs[id] = &protocol.JobState{ID: id, Name: name, StartedAt: time.Now().UTC()}
	} else {
		delete(e.jobs, id)
	}
	jobs := copyMap(e.jobs)
	if started {
		e.logf(protocol.LevelInfo, "job %s started (%s)", id, name)
	} else {
		e.logf(protocol.LevelInfo, "job %s stopped", id)
	}
	e.bus.send(protocol.JobsUpdate(jobs))
	e.mu.Unlock()
	// autoroute only lives to change the route table; when it finishes the
	// table changed, so the poller should look now rather than next interval
	if !started && strings.Contains(name, "manage/autoroute") {
		e.kickRoutePoll()
	}
}

func (e *Engine) monitorError(m *gomsf.EventMonitor, err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	if m != e.monitor {
		e.mu.Unlock()
		return
	}
	e.errStreak++
	streak := e.errStreak
	if streak == 1 { // log once per streak, not per poll
		e.logf(protocol.LevelWarn, "rpc error: %v", err)
	}
	reconnect := streak >= errStreakLimit && e.rpc != nil && e.conn.Status == "connected"
	e.mu.Unlock()
	if reconnect {
		go e.reconnect()
	}
}

func appendCapped(buf, data []byte) []byte {
	buf = append(buf, data...)
	if len(buf) > streamCap {
		buf = buf[len(buf)-streamCap:]
	}
	return buf
}

func hostLabel(host string) string {
	if host == "" {
		return "unknown host"
	}
	return host
}
