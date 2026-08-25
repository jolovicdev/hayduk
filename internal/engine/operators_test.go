package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestOperatorPresenceRefcount(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	e.OperatorJoin("dana")
	e.OperatorJoin("mile") // mile on a second connection
	if got := e.State().Operators; len(got) != 2 || got[0] != "dana" || got[1] != "mile" {
		t.Fatalf("operators %+v", got)
	}
	e.OperatorJoin("dana")
	e.OperatorLeave("dana")
	if got := e.State().Operators; len(got) != 2 {
		t.Fatalf("second connection should keep dana present, got %+v", got)
	}
	e.OperatorLeave("dana")
	if got := e.State().Operators; len(got) != 1 || got[0] != "mile" {
		t.Fatalf("operators %+v", got)
	}
	waitEvent(t, sub, "operator dana joined")
	waitEvent(t, sub, "operator dana left")

	e.OperatorLeave("") // no-op
	if got := e.State().Operators; len(got) != 1 {
		t.Fatalf("operators %+v", got)
	}
}

func TestOperatorEventAttribution(t *testing.T) {
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules", "windows/smb/x")
	fake.setList(gomsf.ModulePayloads, "modules")
	fake.set(gomsf.ModuleInfo, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"name": "x", "rank": "normal"}, nil
	})
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"job_id": 3, "uuid": "u"}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, errBody := e.Exec(context.Background(), "mile", "module.execute",
		json.RawMessage(`{"type":"exploit","name":"windows/smb/x","options":{}}`)); errBody != nil {
		t.Fatalf("module.execute: %+v", errBody)
	}
	waitFor(t, func() bool {
		events := e.State().Events
		return len(events) > 0 && events[len(events)-1].Operator == "mile"
	})
}
