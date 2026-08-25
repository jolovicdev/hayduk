package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// hailLaunch records one module.execute the hail mary fired.
type hailLaunch struct {
	name    string
	options map[string]interface{}
}

func hailFake(t *testing.T) (*fakeRPC, func() []hailLaunch) {
	t.Helper()
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules",
		"windows/smb/a", "windows/smb/b", "multi/http/c", "linux/ssh/d")
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"hosts": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "address": "10.0.0.5", "os_name": "Windows"},
			map[string]interface{}{"host": "10.0.0.6", "address": "10.0.0.6", "os_name": "Linux"},
		}}, nil
	})
	fake.set(gomsf.DbServices, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"services": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "port": 445, "proto": "tcp", "name": "smb"},
			map[string]interface{}{"host": "10.0.0.6", "port": 22, "proto": "tcp", "name": "ssh"},
		}}, nil
	})
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"RHOSTS": map[string]interface{}{
			"type": "string", "required": true, "desc": "The target address range",
		}}, nil
	})
	var mu sync.Mutex
	var executed []hailLaunch
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		executed = append(executed, hailLaunch{name: args[1].(string), options: args[2].(map[string]interface{})})
		mu.Unlock()
		return map[string]interface{}{"job_id": 1, "uuid": "u"}, nil
	})
	launches := func() []hailLaunch {
		mu.Lock()
		defer mu.Unlock()
		return append([]hailLaunch(nil), executed...)
	}
	return fake, launches
}

func TestHailMaryLaunchesMatchesPaced(t *testing.T) {
	fake, _ := hailFake(t)
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	raw, errBody := e.Exec(context.Background(), "dana", "campaign.hail_mary",
		json.RawMessage(`{"hosts":["10.0.0.5","10.0.0.6"],"maxPerHost":1}`))
	if errBody != nil {
		t.Fatalf("hail mary: %+v", errBody)
	}
	var payload protocol.HailMaryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Planned != 2 { // one capped match per host
		t.Fatalf("planned %d, want 2", payload.Planned)
	}

	waitEvent(t, sub, "hail mary on 10.0.0.5: launching 1 exploits")
	deadline := time.After(10 * time.Second)
	for {
		events := e.State().Events
		done, attributed := false, false
		for _, ev := range events {
			if ev == nil {
				continue
			}
			if contains(ev.Text, "hail mary finished: 2 of 2 planned launches") {
				done = true
			}
			if ev.Operator == "dana" {
				attributed = true
			}
		}
		if done {
			if !attributed {
				t.Fatal("hail mary events must carry the operator's name")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("hail mary never finished")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestHailMaryTargetsItsHosts(t *testing.T) {
	fake, launches := hailFake(t)
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, errBody := e.Exec(context.Background(), "", "campaign.hail_mary",
		json.RawMessage(`{"hosts":["10.0.0.5","10.0.0.6"],"maxPerHost":1}`)); errBody != nil {
		t.Fatalf("hail mary: %+v", errBody)
	}
	waitEvent(t, sub, "hail mary finished: 2 of 2 planned launches")

	got := launches()
	if len(got) != 2 {
		t.Fatalf("executed %d launches, want 2: %+v", len(got), got)
	}
	// each exploit must fire at the host it was matched for; without RHOSTS
	// msf launches against nothing and every launch is a guaranteed dud
	targets := map[string]string{}
	for _, l := range got {
		rhosts, ok := l.options["RHOSTS"].(string)
		if !ok || rhosts == "" {
			t.Fatalf("launch of %s carried no RHOSTS: %+v", l.name, l.options)
		}
		if rhosts != "10.0.0.5" && rhosts != "10.0.0.6" {
			t.Fatalf("launch of %s targeted %q", l.name, rhosts)
		}
		targets[rhosts] = l.name
	}
	if len(targets) != 2 {
		t.Fatalf("both hosts must be targeted, got %+v", targets)
	}
}

func TestHailMaryRejectsUnknownHosts(t *testing.T) {
	fake, _ := hailFake(t)
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	if _, errBody := e.Exec(context.Background(), "", "campaign.hail_mary",
		json.RawMessage(`{"hosts":["10.9.9.9"]}`)); errBody == nil || errBody.Code != protocol.CodeBadParams {
		t.Fatalf("unknown host should be bad_params, got %+v", errBody)
	}
	if _, errBody := e.Exec(context.Background(), "", "campaign.hail_mary", json.RawMessage(`{}`)); errBody == nil {
		t.Fatal("empty host list should be bad_params")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
