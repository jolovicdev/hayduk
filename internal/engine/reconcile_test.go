package engine

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// A session seeded at connect can die without the monitor ever reporting a
// close (it never observed the open). reconcileSessions is the path that
// notices, so it must also release the attachment the way sessionClosed
// does, or every browser keeps a live Interact tab bound to a dead session.
func TestReconcileSessionsClearsStaleInteract(t *testing.T) {
	var live atomic.Value
	live.Store(map[string]interface{}{
		"4": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
	})
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return live.Load().(map[string]interface{}), nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	if _, eb := e.Exec(context.Background(), "", "session.attach", json.RawMessage(`{"sid":"4"}`)); eb != nil {
		t.Fatalf("attach: %+v", eb)
	}
	if got := e.State().Interact; got == nil || got.SID != "4" {
		t.Fatalf("precondition: interact %+v", got)
	}

	live.Store(map[string]interface{}{})
	e.reconcileSessions(context.Background())

	if got := e.State().Sessions["4"]; got != nil {
		t.Fatalf("session 4 should be reconciled away, got %+v", got)
	}
	if got := e.State().Interact; got != nil && got.SID != "" {
		t.Fatalf("interact still bound to dead session: %+v", got)
	}
}
