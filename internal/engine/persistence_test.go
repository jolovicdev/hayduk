package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
	"github.com/jolovicdev/hayduk/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func persistentEngine(t *testing.T, fake *fakeRPC, st *store.Store) *Engine {
	t.Helper()
	e := New(Config{
		RPC: fake, Store: st,
		SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour,
	})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

func TestEngineLoadsAuditLogAndContinuesSequence(t *testing.T) {
	st := testStore(t)
	for _, seq := range []int64{41, 42} {
		st.AppendEvent(&protocol.EventEntry{
			Seq: seq, Time: time.Now().UTC(), Level: protocol.LevelInfo,
			Text: "persisted", Workspace: "default",
		})
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}

	fake := stdFake()
	e := New(Config{RPC: fake, Store: st, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	state := e.State()
	if len(state.Events) != 2 || state.Events[1].Seq != 42 {
		t.Fatalf("loaded events %+v", state.Events)
	}
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool {
		events := e.State().Events
		return len(events) > 2 && events[len(events)-1].Seq > 42
	})
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadEvents(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) < 3 || loaded[0].Seq != 41 {
		t.Fatalf("persisted log %+v", loaded)
	}
	var sawAuth bool
	for _, ev := range loaded {
		if strings.Contains(ev.Text, "authenticating") {
			sawAuth = true
		}
	}
	if !sawAuth {
		t.Fatal("new events did not reach the audit log")
	}
}

func handlerFake(t *testing.T) *fakeRPC {
	t.Helper()
	fake := stdFake()
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		if args[0].(string) == "payload" {
			return map[string]interface{}{
				"LHOST": map[string]interface{}{"type": "address", "required": true, "desc": "callback"},
				"LPORT": map[string]interface{}{"type": "integer", "required": true, "desc": "port"},
			}, nil
		}
		return map[string]interface{}{}, nil
	})
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"job_id": 7, "uuid": "u-handler"}, nil
	})
	return fake
}

func TestListenerRegistrationPersistsAndStopsWithJob(t *testing.T) {
	st := testStore(t)
	e := persistentEngine(t, handlerFake(t), st)

	_, eb := e.Exec(context.Background(), "dana", protocol.MethodModuleExecute, json.RawMessage(`{
		"type":"exploit","name":"multi/handler","options":{},
		"payload":"windows/x64/meterpreter/reverse_tcp",
		"payloadOptions":{"LHOST":"10.0.0.99","LPORT":4444}
	}`))
	if eb != nil {
		t.Fatalf("handler launch: %+v", eb)
	}
	listeners := e.State().Listeners
	if len(listeners) != 1 {
		t.Fatalf("listeners %+v", listeners)
	}
	got := listeners[0]
	if got.JobID != "7" || got.Lhost != "10.0.0.99" || got.Lport != 4444 ||
		got.Payload != "windows/x64/meterpreter/reverse_tcp" || got.Operator != "dana" {
		t.Fatalf("listener %+v", got)
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	if saved := st.LoadListeners(); len(saved) != 1 || saved[0].JobID != "7" {
		t.Fatalf("persisted listeners %+v", saved)
	}

	e.jobChanged(e.monitor, "7", "Exploit: multi/handler", false)
	if len(e.State().Listeners) != 0 {
		t.Fatalf("listener survived its job: %+v", e.State().Listeners)
	}
	if saved := st.LoadListeners(); len(saved) != 0 {
		t.Fatalf("stopped listener still persisted: %+v", saved)
	}
}

func TestBootstrapPrunesListenersWhoseJobIsGone(t *testing.T) {
	st := testStore(t)
	if err := st.SaveListeners([]*protocol.ListenerState{
		{JobID: "3", Payload: "linux/x64/meterpreter/reverse_tcp", Lhost: "10.0.0.5", Lport: 4444},
		{JobID: "4", Payload: "windows/x64/meterpreter/reverse_tcp", Lhost: "10.0.0.5", Lport: 4445},
	}); err != nil {
		t.Fatal(err)
	}
	fake := stdFake()
	fake.set(gomsf.JobList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"3": "Exploit: multi/handler"}, nil
	})
	e := persistentEngine(t, fake, st)

	if got := e.State().Listeners; len(got) != 1 || got[0].JobID != "3" {
		t.Fatalf("pruned listeners %+v", got)
	}
	if e.State().Jobs["3"] == nil {
		t.Fatal("live job was not seeded")
	}
	if saved := st.LoadListeners(); len(saved) != 1 || saved[0].JobID != "3" {
		t.Fatalf("pruned registry not persisted: %+v", saved)
	}

	// A disconnect hides live state but must not erase the registry on disk:
	// the next connect reconciles it again.
	e.Disconnect()
	if len(e.State().Listeners) != 0 {
		t.Fatalf("listeners visible while disconnected: %+v", e.State().Listeners)
	}
	if saved := st.LoadListeners(); len(saved) != 1 {
		t.Fatalf("disconnect erased the saved registry: %+v", saved)
	}
}

func TestProfilesPersistWithoutPassword(t *testing.T) {
	st := testStore(t)
	e := New(Config{RPC: stdFake(), Store: st, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)

	raw, eb := e.Exec(context.Background(), "", protocol.MethodProfileList, nil)
	if eb != nil {
		t.Fatalf("profile.list: %+v", eb)
	}
	if string(raw) != "[]" {
		t.Fatalf("empty profile list %s", raw)
	}
	if err := e.Connect(context.Background(), protocol.ConnectParams{
		Host: "10.0.0.5", Port: 55553, Username: "msf", Password: "secret",
	}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	profiles := st.ListProfiles()
	if len(profiles) != 1 || profiles[0].Host != "10.0.0.5" || profiles[0].Port != 55553 {
		t.Fatalf("profiles %+v", profiles)
	}
	data, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "password") {
		t.Fatalf("profile carries a secret: %s", data)
	}
	raw, eb = e.Exec(context.Background(), "", protocol.MethodProfileList, nil)
	if eb != nil {
		t.Fatalf("profile.list after connect: %+v", eb)
	}
	var listed []protocol.ProfilePayload
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Username != "msf" {
		t.Fatalf("listed profiles %+v", listed)
	}
}

func TestRankCacheLoadsAndPersists(t *testing.T) {
	st := testStore(t)
	if err := st.SaveRanks(map[string]string{"exploit/windows/smb/ms17_010": "great"}); err != nil {
		t.Fatal(err)
	}
	e := New(Config{Store: st, RPC: stdFake(), SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	if got := e.State().ModuleRanks["exploit/windows/smb/ms17_010"]; got != "great" {
		t.Fatalf("loaded rank %q", got)
	}
	e.Shutdown()

	st2 := testStore(t)
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules", "windows/smb/a", "windows/smb/b")
	fake.set(gomsf.ModuleInfo, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"name": "n", "rank": "excellent"}, nil
	})
	e2 := persistentEngine(t, fake, st2)
	waitFor(t, func() bool { return len(st2.LoadRanks()) == 2 })
	if got := st2.LoadRanks()["exploit/windows/smb/a"]; got != "excellent" {
		t.Fatalf("persisted rank %q", got)
	}
	_ = e2
}
