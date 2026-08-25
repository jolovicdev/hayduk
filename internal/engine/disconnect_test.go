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

	e.sessionOpened(gomsf.Event{SessionID: "1", Session: &gomsf.Session{
		Type: "shell", TargetHost: "10.0.0.5", ViaExploit: "windows/smb/a",
	}})
	e.jobChanged("7", "aux/scanner/smb/smb_version", true)
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
