package engine

import (
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// A link's sessions and jobs die with it.
func TestDisconnectClearsSessionsAndJobs(t *testing.T) {
	e := connectedEngine(t, stdFake())
	sub := e.Subscribe()
	t.Cleanup(sub.Stop)

	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.sessionOpened(mon, gomsf.Event{SessionID: "1", Session: &gomsf.Session{
		Type: "shell", TargetHost: "10.0.0.5", ViaExploit: "windows/smb/a",
	}})
	e.jobChanged(mon, "7", "aux/scanner/smb/smb_version", true)
	waitFor(t, func() bool {
		st := e.State()
		return len(st.Sessions) == 1 && len(st.Jobs) == 1
	})

	e.Disconnect()

	st := e.State()
	if len(st.Sessions) != 0 {
		t.Fatalf("sessions survived the disconnect: %+v", st.Sessions)
	}
	if len(st.Jobs) != 0 {
		t.Fatalf("jobs survived the disconnect: %+v", st.Jobs)
	}
	// browsers that saw them must hear them go
	deadline := time.After(5 * time.Second)
	sessionsEmpty, jobsEmpty := false, false
	for !(sessionsEmpty && jobsEmpty) {
		select {
		case m := <-sub.C():
			upd, ok := m.(protocol.ResourceUpdate)
			if !ok {
				continue
			}
			if upd.Resource == protocol.ResSessions && len(upd.Sessions) == 0 {
				sessionsEmpty = true
			}
			if upd.Resource == protocol.ResJobs && len(upd.Jobs) == 0 {
				jobsEmpty = true
			}
		case <-deadline:
			t.Fatalf("disconnect never broadcast the emptied state: sessions=%v jobs=%v", sessionsEmpty, jobsEmpty)
		}
	}
}

// The monitor's channel is buffered: events polled just before a disconnect
// can still be drained after the state was cleared, and would resurrect
// ghosts. They belong to a dead monitor and must be dropped, like the
// monitor's errors already are.
// Session IDs are per-link counters, so a reconnect reuses them. A close or
// output event buffered on the dead link's monitor must not delete or splice
// into the replacement link's session of the same ID.
func TestStaleMonitorCloseDoesNotTouchReusedSessionID(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.mu.Lock()
	oldMon := e.monitor
	e.mu.Unlock()

	e.sessionOpened(oldMon, gomsf.Event{SessionID: "1", Session: &gomsf.Session{Type: "shell"}})
	waitFor(t, func() bool { return len(e.State().Sessions) == 1 })

	// the link died and the engine reconnected: bootstrap reset live state and
	// the new link opened its own session 1
	e.mu.Lock()
	newMon := &gomsf.EventMonitor{}
	e.monitor = newMon
	e.sessions = make(map[string]*protocol.SessionState)
	e.interactSID = "1"
	e.mu.Unlock()
	e.sessionOpened(newMon, gomsf.Event{SessionID: "1", Session: &gomsf.Session{Type: "meterpreter"}})
	waitFor(t, func() bool {
		s := e.State().Sessions["1"]
		return s != nil && s.Type == "meterpreter"
	})

	e.sessionClosed(oldMon, gomsf.Event{SessionID: "1"})
	if s := e.State().Sessions["1"]; s == nil {
		t.Fatal("stale monitor close deleted the reused session ID")
	}

	e.sessionOutput(oldMon, gomsf.Event{SessionID: "1", Data: "stale output\r\n"})
	e.mu.Lock()
	stale := len(e.interactOut)
	e.mu.Unlock()
	if stale != 0 {
		t.Fatalf("stale monitor output reached the interact buffer: %d bytes", stale)
	}
}

func TestStaleMonitorEventsDoNotResurrectClearedState(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.Disconnect()

	e.sessionOpened(mon, gomsf.Event{SessionID: "9", Session: &gomsf.Session{Type: "shell"}})
	e.jobChanged(mon, "9", "aux/scanner/http/title", true)

	st := e.State()
	if len(st.Sessions) != 0 {
		t.Fatalf("stale monitor event resurrected session: %+v", st.Sessions)
	}
	if len(st.Jobs) != 0 {
		t.Fatalf("stale monitor event resurrected job: %+v", st.Jobs)
	}
}
