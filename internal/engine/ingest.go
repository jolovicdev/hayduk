package engine

import (
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
	s := ev.Session
	st := &protocol.SessionState{
		ID:       ev.SessionID,
		OpenedAt: time.Now().UTC(),
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
	e.sessions[ev.SessionID] = st
	sessions := copyMap(e.sessions)
	host := hostLabel(st.TargetHost)
	e.logf(protocol.LevelSuccess, "session %s opened (%s) on %s via %s",
		ev.SessionID, st.Type, host, st.ViaExploit)
	e.bus.send(protocol.SessionsUpdate(sessions))
	e.mu.Unlock()
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
	delete(e.sessions, ev.SessionID)
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
