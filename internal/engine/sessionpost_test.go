package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// attachedMeterpreter seeds one meterpreter session, attaches it, and keeps
// the read stream quiet. Tests script SessionMeterpreterWrite themselves.
func attachedMeterpreter(t *testing.T, fake *fakeRPC, sid string) *Engine {
	t.Helper()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			sid: map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
		}, nil
	})
	fake.set(gomsf.SessionMeterpreterRead, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"data": ""}, nil
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions[sid] != nil })
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionAttach, json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("attach: %+v", eb)
	}
	return e
}

func TestSessionPostStreamsThroughTheSession(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	var writes []string
	fake.set(gomsf.SessionMeterpreterWrite, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		writes = append(writes, args[1].(string))
		return map[string]interface{}{"wrote": true}, nil
	})
	e := attachedMeterpreter(t, fake, "4")
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionPost, json.RawMessage(
		`{"sid":"4","module":"multi/recon/local_exploit_suggester","options":{"VERBOSE":true}}`))
	if eb != nil {
		t.Fatalf("session.post: %+v", eb)
	}
	var payload protocol.SessionPostPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	want := "run post/multi/recon/local_exploit_suggester VERBOSE=true"
	if payload.Command != want {
		t.Fatalf("command %q, want %q", payload.Command, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 1 || writes[0] != want+"\n" {
		t.Fatalf("session writes %v", writes)
	}
	// the command echoes into the transcript the operator is watching
	if !strings.Contains(e.State().Interact.Output, want) {
		t.Fatalf("transcript %q", e.State().Interact.Output)
	}
}

func TestSessionPostRejectsInjectionAndUnattached(t *testing.T) {
	fake := stdFake()
	var mu sync.Mutex
	writes := 0
	fake.set(gomsf.SessionMeterpreterWrite, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		writes++
		return map[string]interface{}{"wrote": true}, nil
	})
	e := attachedMeterpreter(t, fake, "4")
	for _, params := range []string{
		`{"sid":"4","module":"multi/gather/env; rm -rf /"}`,
		`{"sid":"4","module":"Multi/Gather/Env"}`,
		`{"sid":"4","module":"multi/gather/env","options":{"X":"a\nrun post/evil"}}`,
		`{"sid":"4","module":"multi/gather/env","options":{"bad name":"x"}}`,
	} {
		if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionPost, json.RawMessage(params)); eb == nil {
			t.Fatalf("accepted %s", params)
		}
	}
	mu.Lock()
	if writes != 0 {
		mu.Unlock()
		t.Fatalf("rejected post commands reached the session (%d writes)", writes)
	}
	mu.Unlock()
	// an unattached session refuses: the output would have nowhere to stream
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionDetach, json.RawMessage(`{"sid":"4"}`)); eb != nil {
		t.Fatalf("detach: %+v", eb)
	}
	_, eb := e.Exec(context.Background(), "", protocol.MethodSessionPost, json.RawMessage(
		`{"sid":"4","module":"multi/gather/env"}`))
	if eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("unattached post: %+v", eb)
	}
}

func TestSessionPostModulesNormalizesAndFallsBack(t *testing.T) {
	fake := stdFake()
	// the fallback reads the index loaded at connect, so seed it up front
	fake.setList(gomsf.ModulePost, "modules", "multi/gather/env")
	fake.set(gomsf.SessionCompatibleModules, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"modules": []interface{}{
			"post/windows/gather/hashdump", "multi/recon/local_exploit_suggester",
		}}, nil
	})
	e := attachedMeterpreter(t, fake, "4")
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionPostModules, json.RawMessage(`{"sid":"4"}`))
	if eb != nil {
		t.Fatalf("post_modules: %+v", eb)
	}
	var payload protocol.SessionPostModulesPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Modules) != 2 || payload.Modules[0] != "multi/recon/local_exploit_suggester" ||
		payload.Modules[1] != "windows/gather/hashdump" {
		t.Fatalf("modules %v", payload.Modules)
	}

	fake.setError(gomsf.SessionCompatibleModules, gomsf.ErrRPC)
	raw, eb = e.Exec(context.Background(), "", protocol.MethodSessionPostModules, json.RawMessage(`{"sid":"4"}`))
	if eb != nil {
		t.Fatalf("fallback post_modules: %+v", eb)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Modules) != 1 || payload.Modules[0] != "multi/gather/env" {
		t.Fatalf("fallback modules %v", payload.Modules)
	}
}
