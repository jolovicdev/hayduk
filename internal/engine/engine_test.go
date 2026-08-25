package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

const protocolCodeUnknown = "unknown_method"

func (e *Engine) testBroadcast() {
	e.mu.Lock()
	e.logf(protocol.LevelInfo, "test event")
	e.mu.Unlock()
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(Config{
		SessionInterval: 10 * time.Millisecond,
		JobInterval:     10 * time.Millisecond,
		OutputInterval:  10 * time.Millisecond,
		RefreshInterval: 10 * time.Millisecond,
		RPC:             &fakeRPC{},
	})
	t.Cleanup(e.Shutdown)
	return e
}

func TestDispatchUnknownMethod(t *testing.T) {
	e := testEngine(t)
	_, err := e.Exec(context.Background(), "", "bogus.method", nil)
	if err == nil || err.Code != protocolCodeUnknown {
		t.Fatalf("want unknown_method, got %+v", err)
	}
}

func TestSubscribeReceivesBroadcast(t *testing.T) {
	e := testEngine(t)
	sub := e.Subscribe()
	defer sub.Stop()

	e.testBroadcast()

	select {
	case m := <-sub.C():
		ev, ok := m.(protocol.EventMsg)
		if !ok {
			t.Fatalf("got %+v", m)
		}
		if ev.Type != "event" {
			t.Fatalf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestSubscribeStopNoLeak(t *testing.T) {
	e := testEngine(t)
	sub := e.Subscribe()
	sub.Stop()
	// engine must not panic or block after a subscriber leaves
	e.testBroadcast()
}

func TestSubscribeAfterShutdownIsClosed(t *testing.T) {
	e := New(Config{RPC: &fakeRPC{}})
	e.Shutdown()
	sub := e.Subscribe()
	defer sub.Stop()

	select {
	case _, ok := <-sub.C():
		if ok {
			t.Fatal("subscription delivered after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription stayed open after shutdown")
	}
}

func TestSubscribeStopDuringShutdown(t *testing.T) {
	e := New(Config{RPC: &fakeRPC{}})
	subs := make([]*Subscription, 1024)
	for i := range subs {
		subs[i] = e.Subscribe()
	}

	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		e.Shutdown()
	}()
	go func() {
		defer wg.Done()
		<-start
		for _, sub := range subs {
			sub.Stop()
		}
	}()
	close(start)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent shutdown did not finish")
	}
}

func TestSlowSubscriberDropped(t *testing.T) {
	e := New(Config{RPC: &fakeRPC{}})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	// pre-fill the subscriber channel to capacity so the next delivery
	// deterministically overflows and drops the subscriber
	for i := 0; i < cap(sub.ch); i++ {
		sub.ch <- protocol.EventMsg{Type: "event", Seq: int64(i)}
	}
	e.testBroadcast()

	// wait for the broadcaster to attempt (and drop) the delivery before
	// draining, otherwise the drain races the overflow check
	deadline := time.After(2 * time.Second)
	for {
		e.bus.mu.Lock()
		n := len(e.bus.subs)
		e.bus.mu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("subscriber was never dropped")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	drained := 0
	for {
		select {
		case _, ok := <-sub.C():
			if !ok {
				return // drained all, then closed: pass
			}
			drained++
		case <-time.After(time.Second):
			t.Fatalf("channel never closed after drop; drained=%d", drained)
		}
	}
}

func TestStateSnapshotIsDeepCopy(t *testing.T) {
	e := testEngine(t)
	e.mu.Lock()
	e.sessions["1"] = &protocol.SessionState{ID: "1"}
	e.mu.Unlock()
	s1 := e.State()
	s1.Sessions["1"].ID = "mutated"
	if e.State().Sessions["1"].ID != "1" {
		t.Fatal("state snapshot aliased engine memory")
	}
}

func waitEvent(t *testing.T, sub *Subscription, substr string) protocol.EventMsg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-sub.C():
			ev, ok := m.(protocol.EventMsg)
			if ok && strings.Contains(ev.Text, substr) {
				return ev
			}
		case <-deadline:
			t.Fatalf("event %q not seen", substr)
		}
	}
}

func okList(key string) func(args ...interface{}) (interface{}, error) {
	return func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{key: []interface{}{}}, nil
	}
}

func TestConnectBootstrap(t *testing.T) {
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules", "windows/smb/a", "windows/smb/b")
	fake.setList(gomsf.ModuleAuxiliary, "modules", "scanner/ssh/x")
	fake.setList(gomsf.ModulePayloads, "modules", "windows/x64/meterpreter/reverse_tcp")
	fake.set(gomsf.DbHosts, okList("hosts"))
	fake.set(gomsf.DbServices, okList("services"))
	fake.set(gomsf.DbCreds, okList("creds"))
	fake.set(gomsf.DbLoots, okList("loots"))
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "3"}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond,
		JobInterval: 10 * time.Millisecond, OutputInterval: 10 * time.Millisecond,
		RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if err := e.Connect(context.Background(), protocol.ConnectParams{
		Host: "h", Port: 55553, Username: "msf", Password: "p"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	st := e.State()
	if st.Connection.Status != "connected" || st.Connection.MSFVersion != "6.5.2" {
		t.Fatalf("connection %+v", st.Connection)
	}
	if st.Modules == nil || len(st.Modules.Exploits) != 2 || len(st.Modules.Payloads) != 1 {
		t.Fatalf("modules %+v", st.Modules)
	}
	if st.Console == nil {
		t.Fatal("console state missing")
	}
	waitEvent(t, sub, "connected to h:55553")
}

func TestConnectBadParams(t *testing.T) {
	e := testEngine(t)
	_, err := e.Exec(context.Background(), "", "connect", json.RawMessage(`{`))
	if err == nil || err.Code != "bad_params" {
		t.Fatalf("want bad_params, got %+v", err)
	}
}

func TestConnectAuthFailure(t *testing.T) {
	fake := &fakeRPC{}
	fake.setError(gomsf.AuthLogin, errors.New("authentication failed"))
	e := New(Config{RPC: fake})
	t.Cleanup(e.Shutdown)
	err := e.Connect(context.Background(), protocol.ConnectParams{})
	if err == nil || err.Code != "connect_failed" {
		t.Fatalf("want connect_failed, got %+v", err)
	}
	if e.State().Connection.Status != "disconnected" {
		t.Fatalf("status %+v", e.State().Connection)
	}
}

func TestSessionOpenCloseIngest(t *testing.T) {
	sessions := map[string]interface{}{}
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		return sessions, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond,
		JobInterval: time.Hour, OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	sessions["4"] = map[string]interface{}{
		"type": "meterpreter", "target_host": "10.0.0.5", "via_exploit": "exploit/x",
		"tunnel_peer": "1.2.3.4:4444-10.0.0.5:1234", "session_host": "10.0.0.5",
	}
	mu.Unlock()
	waitEvent(t, sub, "session 4 opened")
	if e.State().Sessions["4"] == nil {
		t.Fatal("session 4 missing from state")
	}

	mu.Lock()
	delete(sessions, "4")
	mu.Unlock()
	waitEvent(t, sub, "session 4 closed")
	if e.State().Sessions["4"] != nil {
		t.Fatal("session 4 still in state")
	}
}

func TestConsoleOutputIngest(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	out := "\x1b[32mhello\r\nworld\x1b[0m\n"
	prompt := "msf6 > "
	busy := false
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) == 0 || args[0] != "0" { // scripted stream is the operator console's
			return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
		}
		result := map[string]interface{}{"data": out, "prompt": prompt, "busy": busy}
		out = ""
		return result, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond,
		JobInterval: time.Hour, OutputInterval: 10 * time.Millisecond, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case m := <-sub.C():
			if com, ok := m.(protocol.ConsoleOutputMsg); ok && strings.Contains(com.Data, "hello") {
				done = true
			}
		case <-deadline:
			t.Fatal("consoleOutput broadcast not seen")
		}
	}
	if got := e.State().Console.Output; got != "hello\nworld\nmsf6 > " {
		t.Fatalf("console output %q", got)
	}

	mu.Lock()
	busy = true
	mu.Unlock()
	waitFor(t, func() bool { return !strings.HasSuffix(e.State().Console.Output, "msf6 > ") })

	mu.Lock()
	out = "selected module\n"
	prompt = "msf6 auxiliary(scanner/portscan/tcp) > "
	busy = false
	mu.Unlock()
	waitFor(t, func() bool {
		return strings.HasSuffix(e.State().Console.Output, "msf6 auxiliary(scanner/portscan/tcp) > ")
	})
}

func TestRefreshDiscoversNewHost(t *testing.T) {
	hosts := []interface{}{}
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]interface{}{"hosts": hosts}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}
	if len(e.State().Hosts) != 0 {
		t.Fatal("expected empty hosts at start")
	}

	mu.Lock()
	hosts = []interface{}{
		map[string]interface{}{"address": "10.0.0.9", "name": "ws-01", "os_name": "Microsoft Windows 10"},
	}
	mu.Unlock()
	e.refreshDB(context.Background())

	if len(e.State().Hosts) != 1 || e.State().Hosts[0].Address != "10.0.0.9" {
		t.Fatalf("hosts %+v", e.State().Hosts)
	}
	waitEvent(t, sub, "discovered host 10.0.0.9")
}

func TestRefreshNewCredsEvent(t *testing.T) {
	creds := []interface{}{}
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.DbCreds, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		return map[string]interface{}{"creds": creds}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	creds = []interface{}{
		map[string]interface{}{"host": "10.0.0.9", "user": "CORP\\jovan", "pass": "pw", "type": "password"},
		map[string]interface{}{"host": "10.0.0.9", "user": "CORP\\milica", "pass": "pw2", "type": "password"},
	}
	mu.Unlock()
	e.refreshDB(context.Background())
	waitEvent(t, sub, "2 new credentials")
}

func connectedEngine(t *testing.T, fake *fakeRPC) *Engine {
	t.Helper()
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal("condition never met")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestConsoleWriteAndTabs(t *testing.T) {
	fake := stdFake()
	var written []string
	fake.set(gomsf.ConsoleWrite, func(args ...interface{}) (interface{}, error) {
		if args[0] == "0" { // the route console's polls are not operator writes
			written = append(written, args[1].(string))
		}
		return map[string]interface{}{"wrote": "ok"}, nil
	})
	fake.set(gomsf.ConsoleTabs, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"tabs": []interface{}{"version", "vuln"}}, nil
	})
	e := connectedEngine(t, fake)

	if _, err := e.Exec(context.Background(), "", "console.write",
		json.RawMessage(`{"command":"version\n"}`)); err != nil {
		t.Fatalf("write: %+v", err)
	}
	if len(written) != 1 || written[0] != "version\n" {
		t.Fatalf("written %+v", written)
	}

	raw, err := e.Exec(context.Background(), "", "console.tabs", json.RawMessage(`{"line":"ver"}`))
	if err != nil {
		t.Fatal(err)
	}
	var tabs []string
	json.Unmarshal(raw, &tabs)
	if len(tabs) != 2 || tabs[0] != "version" {
		t.Fatalf("tabs %+v", tabs)
	}
}

func TestModuleInfo(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.ModuleInfo, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"name": "MS17-010 EternalBlue", "rank": "great", "description": "d",
			"authors": []interface{}{"a"}, "references": []interface{}{[]interface{}{"CVE", "2017-0143"}},
			"targets": map[string]interface{}{"0": "Automatic"},
		}, nil
	})
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"RHOSTS": map[string]interface{}{
			"type": "string", "required": true, "desc": "The target address range",
		}}, nil
	})
	e := connectedEngine(t, fake)

	raw, err := e.Exec(context.Background(), "", "module.info",
		json.RawMessage(`{"type":"exploit","name":"windows/smb/ms17_010_eternalblue"}`))
	if err != nil {
		t.Fatal(err)
	}
	var info protocol.ModuleInfoPayload
	json.Unmarshal(raw, &info)
	if info.Rank != "great" || len(info.References) != 1 || info.References[0].Value != "2017-0143" {
		t.Fatalf("info %+v", info)
	}

	raw, err = e.Exec(context.Background(), "", "module.options",
		json.RawMessage(`{"type":"exploit","name":"windows/smb/ms17_010_eternalblue"}`))
	if err != nil {
		t.Fatal(err)
	}
	var opts map[string]*protocol.ModuleOptionPayload
	json.Unmarshal(raw, &opts)
	if opts["RHOSTS"] == nil || !opts["RHOSTS"].Required {
		t.Fatalf("options %+v", opts)
	}
}

func okModuleInfo() func(args ...interface{}) (interface{}, error) {
	return func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"name": "n", "rank": "normal"}, nil
	}
}

func TestModuleExecuteRejectsBadEnum(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"ACTION": map[string]interface{}{
			"type": "enum", "required": false, "enums": []interface{}{"SCAN", "EXPLOIT"},
		}}, nil
	})
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	e := connectedEngine(t, fake)

	_, err := e.Exec(context.Background(), "", "module.execute", json.RawMessage(
		`{"type":"auxiliary","name":"scanner/smb/smb_login","options":{"ACTION":"NOPE"}}`))
	if err == nil || err.Code != "invalid_option" {
		t.Fatalf("want invalid_option, got %+v", err)
	}
}

func TestModuleExecuteWithPayload(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		if args[0].(string) == "exploit" {
			return map[string]interface{}{"RHOSTS": map[string]interface{}{
				"type": "string", "required": true, "desc": "target"}}, nil
		}
		return map[string]interface{}{"LHOST": map[string]interface{}{
			"type": "address", "required": true, "desc": "listen"}}, nil
	})
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	var executed map[string]interface{}
	var mu sync.Mutex
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		executed = args[2].(map[string]interface{})
		mu.Unlock()
		return map[string]interface{}{"job_id": 7, "uuid": "u1"}, nil
	})
	e := connectedEngine(t, fake)

	raw, err := e.Exec(context.Background(), "", "module.execute", json.RawMessage(
		`{"type":"exploit","name":"windows/smb/psexec","options":{"RHOSTS":"10.0.0.5"},
		  "payload":"windows/x64/meterpreter/reverse_tcp","payloadOptions":{"LHOST":"1.2.3.4"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var res protocol.ExecPayload
	json.Unmarshal(raw, &res)
	if res.JobID != 7 || res.UUID != "u1" {
		t.Fatalf("res %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if executed["RHOSTS"] != "10.0.0.5" || executed["PAYLOAD"] != "windows/x64/meterpreter/reverse_tcp" {
		t.Fatalf("executed options %+v", executed)
	}
}

func TestSessionAttachWriteStop(t *testing.T) {
	sessions := map[string]interface{}{
		"4": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
	}
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return sessions, nil
	})
	var meterWrites []string
	fake.set(gomsf.SessionMeterpreterWrite, func(args ...interface{}) (interface{}, error) {
		meterWrites = append(meterWrites, args[1].(string))
		return map[string]interface{}{"wrote": "ok"}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })

	if _, err := e.Exec(context.Background(), "", "session.attach", json.RawMessage(`{"sid":"4"}`)); err != nil {
		t.Fatalf("attach: %+v", err)
	}
	if e.State().Interact == nil || e.State().Interact.SID != "4" {
		t.Fatalf("interact %+v", e.State().Interact)
	}

	// writing to an unattached session is rejected
	if _, err := e.Exec(context.Background(), "", "session.write", json.RawMessage(`{"sid":"9","data":"x"}`)); err == nil {
		t.Fatal("write to unattached session should fail")
	}
	if _, err := e.Exec(context.Background(), "", "session.write",
		json.RawMessage(`{"sid":"4","data":"getuid"}`)); err != nil {
		t.Fatalf("write: %+v", err)
	}
	if len(meterWrites) != 1 || meterWrites[0] != "getuid\n" {
		t.Fatalf("meterWrites %+v", meterWrites)
	}

	if _, err := e.Exec(context.Background(), "", "session.detach", nil); err != nil {
		t.Fatal(err)
	}
	if inter := e.State().Interact; inter != nil && inter.SID != "" {
		t.Fatalf("interact not cleared: %+v", inter)
	}
}

func TestAttachSwitchUnwatchesPreviousSession(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"1": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
			"2": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.6"},
		}, nil
	})
	var reads sync.Map // sid -> read count
	fake.set(gomsf.SessionMeterpreterRead, func(args ...interface{}) (interface{}, error) {
		count, _ := reads.LoadOrStore(args[0].(string), new(int32))
		atomic.AddInt32(count.(*int32), 1)
		return map[string]interface{}{"data": ""}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond, JobInterval: time.Hour,
		OutputInterval: 5 * time.Millisecond, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(e.State().Sessions) == 2 })

	if _, err := e.Exec(context.Background(), "", "session.attach", json.RawMessage(`{"sid":"1"}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return readCount(&reads, "1") >= 2 })

	if _, err := e.Exec(context.Background(), "", "session.attach", json.RawMessage(`{"sid":"2"}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return readCount(&reads, "2") >= 2 })

	// session 1 must no longer be polled once session 2 took over
	before := readCount(&reads, "1")
	time.Sleep(150 * time.Millisecond)
	if after := readCount(&reads, "1"); after != before {
		t.Fatalf("session 1 still polled after switching to session 2: %d -> %d reads", before, after)
	}
}

func readCount(m *sync.Map, sid string) int32 {
	if v, ok := m.Load(sid); ok {
		return atomic.LoadInt32(v.(*int32))
	}
	return 0
}

func TestReconnectSendsFullSnapshot(t *testing.T) {
	var fail bool
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("connection refused")
		}
		return map[string]interface{}{}, nil
	})
	hosts := okList("hosts")
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail { // bootstrap of the reconnect attempt sees fresh data
			return map[string]interface{}{"hosts": []interface{}{
				map[string]interface{}{"address": "10.0.0.9", "os_name": "Linux"},
			}}, nil
		}
		return hosts(args...)
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond,
		JobInterval: time.Hour, OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	fail = true
	mu.Unlock()
	waitEvent(t, sub, "reconnected to h:55553")

	// browsers that stayed attached through the blip must get the swapped
	// campaign (modules, db, console), not just the connection state
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-sub.C():
			snap, ok := m.(protocol.Snapshot)
			if !ok {
				continue
			}
			if len(snap.State.Hosts) != 1 || snap.State.Hosts[0].Address != "10.0.0.9" {
				t.Fatalf("snapshot after reconnect carried %+v", snap.State.Hosts)
			}
			return
		case <-deadline:
			t.Fatal("no full snapshot after reconnect")
		}
	}
}

func TestDisconnectDuringConnectAbortsBootstrap(t *testing.T) {
	block := make(chan struct{})
	fake := stdFake()
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		<-block
		return map[string]interface{}{"hosts": []interface{}{}}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	done := make(chan *protocol.ErrorBody, 1)
	go func() {
		done <- e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553})
	}()
	waitFor(t, func() bool { return e.State().Connection.Status == "connecting" })
	e.Disconnect()
	close(block)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("connect aborted by disconnect must not report success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connect never returned")
	}
	// nothing may resurrect the link afterwards
	time.Sleep(150 * time.Millisecond)
	if st := e.State().Connection.Status; st != "disconnected" {
		t.Fatalf("status %q after aborted bootstrap", st)
	}
	if e.State().Console != nil {
		t.Fatal("aborted bootstrap must not expose console state")
	}
}

func TestDisconnectDestroysBothConsoles(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	destroyed := map[string]bool{}
	fake.set(gomsf.ConsoleDestroy, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 0 {
			destroyed[fmt.Sprint(args[0])] = true
		}
		return map[string]interface{}{"result": "success"}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.State().Connection.Status == "connected" })
	e.Disconnect()

	mu.Lock()
	defer mu.Unlock()
	// stdFake numbers consoles from "0": the operator console and the
	// route-poll console must both be destroyed on the way out
	if !destroyed["0"] || !destroyed["1"] {
		t.Fatalf("disconnect must destroy both consoles, destroyed=%v", destroyed)
	}
}

func TestStaleReconnectCannotOverwriteNewerConnection(t *testing.T) {
	var mu sync.Mutex
	fail := true
	blocked := make(chan struct{})
	release := make(chan struct{})
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("connection refused")
		}
		return map[string]interface{}{}, nil
	})
	// the reconnect attempt's bootstrap stalls in console.create (only
	// bootstraps call it, so no stray refresh can steal the stall slot);
	// the manual connect that follows must sail through
	var consoles int32
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		// call #3 is the reconnect bootstrap's first console
		if atomic.AddInt32(&consoles, 1) == 3 {
			close(blocked)
			<-release
		}
		return map[string]interface{}{"id": "9"}, nil
	})
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"hosts": []interface{}{}}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	// each bootstrap reads a distinct version, so whichever attempt commits
	// last is visible in the connection state
	var bootstraps int32
	fake.set(gomsf.CoreVersion, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"version": fmt.Sprintf("6.%d", atomic.AddInt32(&bootstraps, 1)), "ruby": "3.3", "api": "1.0"}, nil
	})

	// the initial connect must not hit the stalling db.hosts handler
	mu.Lock()
	fail = false
	mu.Unlock()
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553}); err != nil {
		t.Fatal(err)
	}

	// rpc errors push the engine into its reconnect loop; its bootstrap stalls
	mu.Lock()
	fail = true
	mu.Unlock()
	waitEvent(t, sub, "connection lost; reconnecting")
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnect bootstrap never started")
	}

	// operator gives up and connects fresh while the stale bootstrap hangs
	e.Disconnect()
	mu.Lock()
	fail = false
	mu.Unlock()
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h2", Port: 55553}); err != nil {
		t.Fatalf("manual connect: %+v", err)
	}
	if st := e.State().Connection; st.Status != "connected" || st.Host != "h2" {
		t.Fatalf("manual connect did not stick: %+v", st)
	}

	close(release) // stale bootstrap finishes now; it must be discarded
	time.Sleep(300 * time.Millisecond)
	st := e.State().Connection
	if st.Status != "connected" || st.Host != "h2" {
		t.Fatalf("stale reconnect overwrote the newer connection: %+v", st)
	}
	if st.MSFVersion != "6.3" {
		t.Fatalf("stale bootstrap (msf %s) committed over the manual connect; want 6.3", st.MSFVersion)
	}
	if e.State().Console == nil {
		t.Fatal("newer connection must keep its console")
	}
}

func TestDBRefreshSurfacesFailure(t *testing.T) {
	fake := stdFake()
	e := connectedEngine(t, fake)
	fake.setError(gomsf.DbHosts, fmt.Errorf("%w: database not connected", gomsf.ErrRPC))

	_, errBody := e.Exec(context.Background(), "", "db.refresh", nil)
	if errBody == nil {
		t.Fatal("db.refresh must fail when the underlying refresh fails")
	}
	if errBody.Code != protocol.CodeRPC {
		t.Fatalf("want rpc_error, got %+v", errBody)
	}
}

func TestBusQueueReleasesBackingStorage(t *testing.T) {
	b := newBus()
	done := make(chan struct{})
	go func() {
		b.run()
		close(done)
	}()
	const n = 100_000
	for i := 0; i < n; i++ {
		b.send(struct{}{})
	}
	deadline := time.After(5 * time.Second)
	for {
		b.mu.Lock()
		l, c := len(b.queue), cap(b.queue)
		b.mu.Unlock()
		if l == 0 {
			if c != 0 {
				t.Fatalf("drained queue kept backing array of cap %d", c)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("queue never drained: %d left", l)
		case <-time.After(5 * time.Millisecond):
		}
	}
	b.shutdown()
	<-done
}

func TestRequiredParamsRejected(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"4": map[string]interface{}{"type": "shell", "target_host": "10.0.0.5"}}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	e.Exec(context.Background(), "", "session.attach", json.RawMessage(`{"sid":"4"}`))

	cases := []struct{ name, method, params string }{
		{"connect empty host", "connect", `{"host":"","port":55553,"username":"msf","password":"p"}`},
		{"connect zero port", "connect", `{"host":"127.0.0.1","port":0,"username":"msf","password":"p"}`},
		{"connect port out of range", "connect", `{"host":"127.0.0.1","port":70000,"username":"msf","password":"p"}`},
		{"console.write empty", "console.write", `{"command":""}`},
		{"module.info no name", "module.info", `{"type":"exploit","name":""}`},
		{"module.options no type", "module.options", `{"type":"","name":"x"}`},
		{"module.execute no name", "module.execute", `{"type":"exploit","name":""}`},
		{"payloads no name", "module.compatible_payloads", `{"type":"exploit","name":""}`},
		{"session.attach empty sid", "session.attach", `{"sid":""}`},
		{"session.write empty sid", "session.write", `{"sid":"","data":"x"}`},
		{"session.write empty data", "session.write", `{"sid":"4","data":""}`},
		{"session.stop empty sid", "session.stop", `{"sid":""}`},
		{"session.upgrade empty lhost", "session.upgrade", `{"sid":"4","lhost":"","lport":4444}`},
		{"session.upgrade zero lport", "session.upgrade", `{"sid":"4","lhost":"10.0.0.1","lport":0}`},
		{"workspace.set empty", "workspace.set", `{"name":""}`},
		{"attacks.find empty", "attacks.find", `{"host":""}`},
	}
	for _, tc := range cases {
		_, errBody := e.Exec(context.Background(), "", tc.method, json.RawMessage(tc.params))
		if errBody == nil || errBody.Code != protocol.CodeBadParams {
			t.Errorf("%s: want bad_params, got %+v", tc.name, errBody)
		}
	}
}

func TestJobTrackingCarriesStartTime(t *testing.T) {
	var mu sync.Mutex
	jobs := map[string]interface{}{}
	fake := stdFake()
	fake.set(gomsf.JobList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		return jobs, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: 10 * time.Millisecond,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	jobs["7"] = "Exploit: linux/http/apache_druid_js_rce"
	mu.Unlock()
	waitEvent(t, sub, "job 7 started")
	st := e.State().Jobs["7"]
	if st == nil {
		t.Fatal("job 7 missing from state")
	}
	if st.Name != "Exploit: linux/http/apache_druid_js_rce" {
		t.Fatalf("job name %q", st.Name)
	}
	if st.StartedAt.IsZero() {
		t.Fatal("job must carry the moment it was first seen")
	}

	mu.Lock()
	delete(jobs, "7")
	mu.Unlock()
	waitEvent(t, sub, "job 7 stopped")
	if e.State().Jobs["7"] != nil {
		t.Fatal("job 7 still in state after stopping")
	}
}

func TestLaunchEventsAreSingleLine(t *testing.T) {
	var jobID int
	fake := stdFake()
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		jobID++
		return map[string]interface{}{"job_id": jobID, "uuid": "u"}, nil
	})
	e := connectedEngine(t, fake)
	sub := e.Subscribe()
	defer sub.Stop()

	exec := func(name string) {
		t.Helper()
		if _, errBody := e.Exec(context.Background(), "", "module.execute",
			json.RawMessage(`{"type":"exploit","name":"`+name+`","options":{}}`)); errBody != nil {
			t.Fatalf("%s: %+v", name, errBody)
		}
	}

	exec("a") // job 1: a watchable job
	waitEvent(t, sub, "exploit/a launched as job 1")

	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"job_id": 0, "uuid": "u"}, nil
	})
	exec("b") // job 0: msf ran it inline, there is no job to watch
	waitEvent(t, sub, "exploit/b ran inline")

	// no pre-launch chatter events may remain either
	for _, ev := range e.State().Events {
		if ev != nil && strings.Contains(ev.Text, "launching exploit/") {
			t.Fatalf("launch events must be single-line: %q", ev.Text)
		}
	}
}

func TestConnectErrorPathGenRace(t *testing.T) {
	// bootstrap fails slowly; Disconnect bumps e.gen in a tight loop the
	// whole time. The error path must read e.gen only under the mutex or
	// the race detector fires on the connect-status broadcast decision.
	fake := &fakeRPC{}
	fake.set(gomsf.AuthLogin, func(args ...interface{}) (interface{}, error) {
		time.Sleep(2 * time.Millisecond)
		return nil, errors.New("nope")
	})
	e := New(Config{RPC: fake})
	t.Cleanup(e.Shutdown)

	done := make(chan struct{})
	var hammer sync.WaitGroup
	hammer.Add(1)
	go func() {
		defer hammer.Done()
		for {
			select {
			case <-done:
				return
			default:
				e.Disconnect()
			}
		}
	}()
	for i := 0; i < 200; i++ {
		_ = e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553})
	}
	close(done)
	hammer.Wait()
}

func TestBusOverflowClosesSubscribers(t *testing.T) {
	b := newBus()
	id1, ch1 := b.subscribe()
	id2, ch2 := b.subscribe()
	s1 := &Subscription{ch: ch1, id: id1, bus: b}
	s2 := &Subscription{ch: ch2, id: id2, bus: b}
	defer s1.Stop()
	defer s2.Stop()

	// producers outrun the broadcaster and blow past the cap
	for i := 0; i < busQueueCap+100; i++ {
		b.send(protocol.EventMsg{Type: protocol.KindEvent, Seq: int64(i)})
	}

	done := make(chan struct{})
	go func() {
		b.run()
		close(done)
	}()

	// both subscribers must be closed, not left with a silently lossy feed;
	// closing is what makes clients reconnect and take a fresh snapshot
	for _, ch := range []<-chan protocol.ServerMessage{s1.C(), s2.C()} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatal("overflow must close subscribers, not deliver backlog")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("overflow never closed a subscriber; clients would stay stale")
		}
	}

	// the queue must be cleared: a subscriber joining after the overflow
	// sees only post-overflow messages, never stale backlog
	b.mu.Lock()
	for len(b.queue) != 0 {
		b.mu.Unlock()
		time.Sleep(time.Millisecond)
		b.mu.Lock()
	}
	b.mu.Unlock()
	id3, ch3 := b.subscribe()
	s3 := &Subscription{ch: ch3, id: id3, bus: b}
	defer s3.Stop()
	b.send(protocol.EventMsg{Type: protocol.KindEvent, Seq: -1})
	select {
	case m := <-s3.C():
		if m.(protocol.EventMsg).Seq != -1 {
			t.Fatalf("post-overflow subscriber saw stale backlog seq %d", m.(protocol.EventMsg).Seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("post-overflow subscriber starved")
	}

	b.shutdown()
	<-done
}

func TestReconnectAfterPersistentErrors(t *testing.T) {
	var fail bool
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, errors.New("connection refused")
		}
		return map[string]interface{}{}, nil
	})
	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond,
		JobInterval: time.Hour, OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	fail = true
	mu.Unlock()
	waitEvent(t, sub, "connection lost; reconnecting")

	mu.Lock()
	fail = false
	mu.Unlock()
	waitEvent(t, sub, "reconnected to h:55553")
	if e.State().Connection.Status != "connected" {
		t.Fatalf("status %+v", e.State().Connection)
	}
}

func TestConnectEmitsProgressEvents(t *testing.T) {
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules", "a", "b")
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553}); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, sub, "authenticating against h:55553")
	waitEvent(t, sub, "loading module list")
	waitEvent(t, sub, "console ready")
}

func TestStateCollectionsNeverNil(t *testing.T) {
	e := New(Config{RPC: &fakeRPC{}})
	t.Cleanup(e.Shutdown)
	s := e.State()
	if s.Hosts == nil || s.Services == nil || s.Creds == nil || s.Loot == nil || s.Events == nil {
		t.Fatal("slice collections must marshal as empty, not null")
	}
	if s.Sessions == nil || s.Jobs == nil {
		t.Fatal("map collections must marshal as empty, not null")
	}
}

func TestModuleExecutePayloadFailureNoCrash(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		if args[0].(string) == "exploit" {
			return map[string]interface{}{"RHOSTS": map[string]interface{}{"type": "string", "required": true}}, nil
		}
		return map[string]interface{}{
			"LHOST":              map[string]interface{}{"type": "address", "required": true},
			"LPORT":              map[string]interface{}{"type": "port", "required": true, "default": 4444},
			"AutoLoadExtensions": map[string]interface{}{"type": "string", "default": []interface{}{"stdapi"}},
		}, nil
	})
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	var executed map[string]interface{}
	var mu sync.Mutex
	execErr := fmt.Errorf("%w: Invalid module option value for AutoLoadExtensions: must be a scalar", gomsf.ErrRPC)
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		executed = args[2].(map[string]interface{})
		mu.Unlock()
		return nil, execErr
	})
	e := connectedEngine(t, fake)

	_, errBody := e.Exec(context.Background(), "", "module.execute", json.RawMessage(
		`{"type":"exploit","name":"multi/handler","options":{},
		  "payload":"linux/x64/meterpreter/reverse_tcp","payloadOptions":{"LHOST":"127.0.0.1","LPORT":4444}}`))
	if errBody == nil {
		t.Fatal("rpc failure must surface as an error, not a crash")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := executed["AutoLoadExtensions"]; ok {
		t.Fatal("array option default must be dropped before execute")
	}
	if executed["PAYLOAD"] != "linux/x64/meterpreter/reverse_tcp" || executed["LHOST"] != "127.0.0.1" {
		t.Fatalf("executed options %+v", executed)
	}
	if lp, ok := executed["LPORT"].(int64); !ok || lp != 4444 {
		t.Fatalf("LPORT must arrive an integer, not a float: %T %v", executed["LPORT"], executed["LPORT"])
	}
}

func TestRefreshBroadcastsContentChanges(t *testing.T) {
	host := func(os string) func(args ...interface{}) (interface{}, error) {
		return func(args ...interface{}) (interface{}, error) {
			return map[string]interface{}{"hosts": []interface{}{map[string]interface{}{
				"host": "10.0.0.5", "address": "10.0.0.5", "os_name": os,
			}}}, nil
		}
	}
	fake := stdFake()
	fake.set(gomsf.DbHosts, host("Linux"))
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}

	// same count, changed fingerprint: the update must still reach browsers
	fake.set(gomsf.DbHosts, host("Ubuntu 22.04"))
	e.refreshDB(context.Background())
	deadline := time.After(5 * time.Second)
	for {
		sawUpdate := false
		for done := false; !done; {
			select {
			case m := <-sub.C():
				if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResHosts {
					sawUpdate = true
					done = true
				}
			case <-deadline:
				t.Fatal("os fingerprint change with equal host count never broadcast")
			}
		}
		if sawUpdate {
			break
		}
	}
	if got := e.State().Hosts[0].OSName; got != "Ubuntu 22.04" {
		t.Fatalf("osName %q", got)
	}

	// identical refresh must stay quiet
	e.refreshDB(context.Background())
	select {
	case m := <-sub.C():
		t.Fatalf("unchanged refresh broadcast %v", m)
	case <-time.After(300 * time.Millisecond):
	}
}
