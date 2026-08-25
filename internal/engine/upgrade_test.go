package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestSessionUpgrade(t *testing.T) {
	fake := stdFake()
	upgraded := make(chan []interface{}, 1)
	fake.set(gomsf.SessionShellUpgrade, func(args ...interface{}) (interface{}, error) {
		upgraded <- args
		return map[string]interface{}{}, nil
	})
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"1": map[string]interface{}{"type": "shell", "session_host": "10.0.0.5", "tunnel_peer": "10.0.0.5"},
			"2": map[string]interface{}{"type": "meterpreter", "session_host": "10.0.0.6"},
		}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: 10 * time.Millisecond, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return len(e.State().Sessions) == 2 })

	if eb := e.sessionUpgrade(context.Background(), "dana", protocol.SessionUpgradeParams{SID: "1", LHOST: "10.0.0.99", LPORT: 4444}); eb != nil {
		t.Fatalf("upgrade: %+v", eb)
	}
	select {
	case args := <-upgraded:
		if len(args) != 3 || args[0] != "1" || args[1] != "10.0.0.99" || args[2] != 4444 {
			t.Fatalf("upgrade args %v", args)
		}
	default:
		t.Fatal("session.shell_upgrade not called")
	}
	waitEvent(t, sub, "session 1 upgrading to meterpreter via 10.0.0.99:4444")

	if eb := e.sessionUpgrade(context.Background(), "dana", protocol.SessionUpgradeParams{SID: "2", LHOST: "h", LPORT: 1}); eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("meterpreter sessions must not upgrade, got %+v", eb)
	}
	if eb := e.sessionUpgrade(context.Background(), "dana", protocol.SessionUpgradeParams{SID: "9", LHOST: "h", LPORT: 1}); eb == nil || eb.Code != protocol.CodeSessionNotFound {
		t.Fatalf("missing session, got %+v", eb)
	}
}
