package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Team mode parity with Armitage: every operator gets a private console and
// their own session views, while transcripts, events and chat stay shared.

func teamEngine(t *testing.T, fake *fakeRPC) *Engine {
	t.Helper()
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: 10 * time.Millisecond, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

func consoleCount(e *Engine) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.consoles)
}

// Each operator's writes and daemon output land in their own console only;
// broadcasts carry the owner so sockets can scope them.
func TestTeamConsolesArePrivate(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	danaOut := ""
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 0 && args[0] == "2" { // dana's console, third created
			data := danaOut
			danaOut = ""
			return map[string]interface{}{"data": data, "prompt": "msf6 > ", "busy": false}, nil
		}
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})
	e := teamEngine(t, fake)
	sub := e.Subscribe()
	defer sub.Stop()

	e.OperatorJoin("dana")
	e.OperatorJoin("mile")
	waitFor(t, func() bool { return consoleCount(e) == 3 }) // "", dana, mile

	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodConsoleWrite,
		json.RawMessage(`{"command":"version\n"}`)); eb != nil {
		t.Fatalf("dana write: %+v", eb)
	}
	if got := e.ConsoleFor("dana").Output; !strings.Contains(got, "version") {
		t.Fatalf("dana console missing her own echo: %q", got)
	}
	if got := e.ConsoleFor("mile").Output; strings.Contains(got, "version") {
		t.Fatalf("dana's command leaked into mile's console: %q", got)
	}
	if got := e.ConsoleFor("").Output; strings.Contains(got, "version") {
		t.Fatalf("dana's command leaked into the local console: %q", got)
	}

	// the echo broadcast is scoped to dana
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-sub.C():
			if out, ok := m.(protocol.ConsoleOutputMsg); ok && strings.Contains(out.Data, "version") {
				if out.Operator != "dana" {
					t.Fatalf("echo broadcast scoped to %q, want dana", out.Operator)
				}
				goto echoed
			}
		case <-deadline:
			t.Fatal("dana's echo never broadcast")
		}
	}
echoed:

	// daemon output on dana's console reaches dana alone
	mu.Lock()
	danaOut = "Framework: 6.4.0\n"
	mu.Unlock()
	waitFor(t, func() bool {
		return strings.Contains(e.ConsoleFor("dana").Output, "Framework:")
	})
	if got := e.ConsoleFor("mile").Output; strings.Contains(got, "Framework:") {
		t.Fatalf("dana's console output leaked into mile's console: %q", got)
	}
}

// Presence owns the console: a join creates it, a rejoin inside the linger
// resumes it, and the linger's expiry destroys it on the daemon.
func TestTeamConsoleLifecycleLinger(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	created := 0
	destroyed := map[string]bool{}
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		id := created
		created++
		return map[string]interface{}{"id": strconv.Itoa(id)}, nil
	})
	fake.set(gomsf.ConsoleDestroy, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 0 {
			destroyed[args[0].(string)] = true
		}
		return map[string]interface{}{"result": "success"}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, ConsoleLinger: 50 * time.Millisecond})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	e.OperatorJoin("dana")
	waitFor(t, func() bool { return consoleCount(e) == 2 })
	e.mu.Lock()
	first := e.consoles["dana"]
	e.mu.Unlock()

	// a refresh-style blip: leave and rejoin inside the linger keeps the
	// same console, transcript and daemon id
	e.OperatorLeave("dana")
	e.OperatorJoin("dana")
	e.mu.Lock()
	again := e.consoles["dana"]
	e.mu.Unlock()
	if again != first {
		t.Fatal("rejoin inside the linger must resume the same console")
	}
	mu.Lock()
	creates := created
	mu.Unlock()
	if creates != 3 { // "" + route + dana, no fourth
		t.Fatalf("rejoin created a new console: %d creates", creates)
	}

	// the final departure drops it after the linger
	e.OperatorLeave("dana")
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return destroyed["2"]
	})
	if got := e.ConsoleFor("dana"); got != nil {
		t.Fatalf("console survived the linger: %+v", got)
	}
}

// One operator's attach never moves another's view; transcripts are shared
// and survive until the last holder detaches.
func TestTeamAttachIsolationSharedTranscript(t *testing.T) {
	e := testEngine(t)
	e.sessions["1"] = &protocol.SessionState{ID: "1", Type: "shell"}
	e.sessions["2"] = &protocol.SessionState{ID: "2", Type: "shell"}
	sub := e.Subscribe()
	defer sub.Stop()

	if eb := e.attach("dana", "1"); eb != nil {
		t.Fatalf("dana attach: %+v", eb)
	}
	if eb := e.attach("mile", "2"); eb != nil {
		t.Fatalf("mile attach: %+v", eb)
	}
	if got := e.InteractFor("dana"); got == nil || got.SID != "1" {
		t.Fatalf("dana view %+v", got)
	}
	if got := e.InteractFor("mile"); got == nil || got.SID != "2" {
		t.Fatalf("mile view %+v", got)
	}

	// the attach broadcasts are scoped to their operator
	seen := map[string]string{}
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case m := <-sub.C():
			if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResInteract && up.Interact != nil {
				seen[up.Operator] = up.Interact.SID
			}
		case <-deadline:
			t.Fatalf("attach broadcasts missing: %v", seen)
		}
	}
	if seen["dana"] != "1" || seen["mile"] != "2" {
		t.Fatalf("attach broadcasts mis-scoped: %v", seen)
	}

	e.sessionOutput(nil, gomsf.Event{SessionID: "1", Data: "shared history\n"})
	waitFor(t, func() bool {
		return strings.Contains(string(e.interactBufs["1"]), "shared history")
	})

	// mile opens the same session later and replays dana's history
	if eb := e.attach("mile", "1"); eb != nil {
		t.Fatalf("mile attach 1: %+v", eb)
	}
	if got := e.InteractFor("mile"); got == nil || !strings.Contains(got.Output, "shared history") {
		t.Fatalf("mile did not replay the shared transcript: %+v", got)
	}

	// dana closes her tab: mile's transcript and attachment survive
	e.detach("dana", "1")
	if got := e.InteractFor("dana"); got != nil {
		t.Fatalf("dana view survived detach: %+v", got)
	}
	e.mu.Lock()
	_, held := e.interactBufs["1"]
	mileHolds := e.attached["mile"]["1"]
	e.mu.Unlock()
	if !held || !mileHolds {
		t.Fatal("dana's detach dropped mile's shared transcript")
	}
	e.sessionOutput(nil, gomsf.Event{SessionID: "1", Data: "still live\n"})
	waitFor(t, func() bool {
		return strings.Contains(string(e.interactBufs["1"]), "still live")
	})

	// the last holder's detach ends the transcript
	e.detach("mile", "1")
	e.mu.Lock()
	_, held = e.interactBufs["1"]
	e.mu.Unlock()
	if held {
		t.Fatal("transcript survived the last detach")
	}
}

// A session close clears every operator's view of it, each with their own
// scoped interact update.
func TestTeamSessionCloseClearsAllViews(t *testing.T) {
	e := testEngine(t)
	e.sessions["1"] = &protocol.SessionState{ID: "1", Type: "shell"}
	if eb := e.attach("dana", "1"); eb != nil {
		t.Fatal(eb)
	}
	if eb := e.attach("mile", "1"); eb != nil {
		t.Fatal(eb)
	}
	sub := e.Subscribe()
	defer sub.Stop()

	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.sessionClosed(mon, gomsf.Event{SessionID: "1"})

	if e.InteractFor("dana") != nil || e.InteractFor("mile") != nil {
		t.Fatal("session close left an operator view behind")
	}
	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case m := <-sub.C():
			if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResInteract &&
				up.Interact != nil && up.Interact.SID == "" {
				seen[up.Operator] = true
			}
		case <-deadline:
			t.Fatalf("per-operator interact clears missing: %v", seen)
		}
	}
	if !seen["dana"] || !seen["mile"] {
		t.Fatalf("interact clears mis-scoped: %v", seen)
	}
}

// Chat rides the shared event log with attribution, works while the link is
// down, and stays out of client reports.
func TestTeamChat(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodTeamChat,
		json.RawMessage(`{"text":"  pushing the pivot now  "}`)); eb != nil {
		t.Fatalf("chat: %+v", eb)
	}
	ev := waitEvent(t, sub, "pushing the pivot now")
	if ev.Level != protocol.LevelChat || ev.Operator != "dana" {
		t.Fatalf("chat event %+v", ev)
	}

	for _, bad := range []string{`{"text":""}`, `{"text":"   "}`} {
		if _, eb := e.Exec(context.Background(), "dana", protocol.MethodTeamChat,
			json.RawMessage(bad)); eb == nil || eb.Code != protocol.CodeBadParams {
			t.Fatalf("empty chat accepted: %+v", eb)
		}
	}
	long := `{"text":"` + strings.Repeat("x", chatMaxLen+1) + `"}`
	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodTeamChat,
		json.RawMessage(long)); eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("overlong chat accepted: %+v", eb)
	}
	if _, eb := e.Exec(context.Background(), "", protocol.MethodTeamChat,
		json.RawMessage(`{"text":"hi"}`)); eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("anonymous chat accepted: %+v", eb)
	}

	html, eb := e.reportDocument()
	if eb != nil {
		t.Fatalf("report: %+v", eb)
	}
	if strings.Contains(html, "pushing the pivot now") {
		t.Fatal("team chat leaked into the client report")
	}
}

// The operator whose launch opened a session gets the credit in the shared
// event log and on the session itself.
func TestTeamSessionOpenAttribution(t *testing.T) {
	fake := stdFake()
	var listCalls int32
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		atomic.AddInt32(&listCalls, 1)
		return map[string]interface{}{}, nil
	})
	e := connectedEngine(t, fake)
	// the connect-time sweep must take its snapshot before the synthetic
	// open: a later snapshot would prune the session the daemon never lists
	waitFor(t, func() bool { return atomic.LoadInt32(&listCalls) >= 2 })
	sub := e.Subscribe()
	defer sub.Stop()

	key := e.beginExploitRun("exploit/multi/ssh/sshexec", "default", "dana")
	e.armExploitRun(key, "uuid-1", 0)

	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.sessionOpened(mon, gomsf.Event{SessionID: "1", Session: &gomsf.Session{
		Type: "shell", TargetHost: "10.0.0.9",
		ViaExploit: "exploit/multi/ssh/sshexec", ExploitUUID: "uuid-1",
	}})

	ev := waitEvent(t, sub, "session 1 opened")
	if ev.Operator != "dana" {
		t.Fatalf("session open attributed to %q, want dana", ev.Operator)
	}
	waitFor(t, func() bool {
		s := e.State().Sessions["1"]
		return s != nil && s.OpenedBy == "dana"
	})
}

// One operator's disconnect drops the shared link and says who did it.
func TestTeamDisconnectAttribution(t *testing.T) {
	e := connectedEngine(t, stdFake())
	sub := e.Subscribe()
	defer sub.Stop()
	e.DisconnectAs("dana")
	ev := waitEvent(t, sub, "disconnected the team")
	if ev.Operator != "dana" || ev.Level != protocol.LevelWarn {
		t.Fatalf("disconnect event %+v", ev)
	}
}

// An operator who joins while a connect is still bootstrapping gets a
// private console on the new link from the post-commit sweep.
func TestTeamLateJoinerGetsConsole(t *testing.T) {
	fake := stdFake()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fake.set(gomsf.DbLoots, func(args ...interface{}) (interface{}, error) {
		once.Do(func() { close(entered) })
		<-release
		return map[string]interface{}{"loots": []interface{}{}}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	done := make(chan struct{})
	go func() {
		if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
			t.Errorf("connect: %+v", err)
		}
		close(done)
	}()
	<-entered
	e.OperatorJoin("dana") // rpc not committed yet: no console can exist
	close(release)
	<-done

	waitFor(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.consoles["dana"] != nil
	})
}

// Departed operators' session attachments release after the linger, and a
// rejoin inside it keeps them.
func TestTeamLingerReleasesAttachments(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"1": map[string]interface{}{"type": "shell", "session_host": "10.0.0.1"},
		}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, ConsoleLinger: 50 * time.Millisecond})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	e.mu.Lock()
	e.sessions["1"] = &protocol.SessionState{ID: "1", Type: "shell"}
	e.mu.Unlock()

	e.OperatorJoin("dana")
	if eb := e.attach("dana", "1"); eb != nil {
		t.Fatal(eb)
	}
	e.OperatorLeave("dana")
	// inside the linger the attachment survives for the rejoin
	e.mu.Lock()
	held := e.attached["dana"]["1"]
	e.mu.Unlock()
	if !held {
		t.Fatal("attachment dropped before the linger expired")
	}
	waitFor(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.attached["dana"] == nil && e.interactBufs["1"] == nil
	})
}
