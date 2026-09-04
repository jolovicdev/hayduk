package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// gomsf calls through a nil RPCCaller and panics, so a command that loses its
// connection mid-flight must answer not_connected instead.
func TestModuleExecuteAfterDisconnectIsAnErrorNotACrash(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.Disconnect()

	_, eb := e.moduleExecute(context.Background(), e.connectedRPC(), "dana",
		protocol.ModuleExecuteParams{Type: "exploit", Name: "windows/smb/a"})
	if eb == nil || eb.Code != protocol.CodeNotConnected {
		t.Fatalf("want not_connected after disconnect, got %+v", eb)
	}
}

func TestCommandAfterDisconnectAnswersNotConnected(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.Disconnect()

	for _, method := range []string{
		protocol.MethodModuleInfo, protocol.MethodModuleOptions, protocol.MethodPayloads,
		protocol.MethodWorkspaceList,
	} {
		_, eb := e.Exec(context.Background(), "dana", method,
			json.RawMessage(`{"type":"exploit","name":"windows/smb/a"}`))
		if eb == nil || eb.Code != protocol.CodeNotConnected {
			t.Fatalf("%s after disconnect: want not_connected, got %+v", method, eb)
		}
	}
}

// workspace.set must not serve the old workspace's data when the refresh
// after the switch fails: msf has already moved, so the engine updates the
// connection resource, clears the old workspace's db resources, broadcasts
// the clear, and answers with the refresh error instead of swallowing it.
func TestWorkspaceSetReportsRefreshFailureAndClearsState(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.DbSetWorkspace, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"hosts": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "address": "10.0.0.5", "os_name": "Linux"},
		}}, nil
	})
	fake.set(gomsf.DbServices, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"services": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "port": 22, "proto": "tcp", "name": "ssh"},
		}}, nil
	})
	e := connectedEngine(t, fake)
	if st := e.State(); len(st.Hosts) != 1 || len(st.Services) != 1 {
		t.Fatalf("setup: want 1 host and 1 service before the switch, got %d/%d", len(st.Hosts), len(st.Services))
	}
	sub := e.Subscribe()
	defer sub.Stop()

	// the switch lands on msf, then the refresh that should repopulate
	// state dies on its first read
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return nil, fmt.Errorf("%w: workspace table unreadable", gomsf.ErrRPC)
	})

	_, eb := e.Exec(context.Background(), "dana", protocol.MethodWorkspaceSet, json.RawMessage(`{"name":"customer-a"}`))
	if eb == nil || eb.Code != protocol.CodeRPC {
		t.Fatalf("want rpc error from the failed refresh, got %+v", eb)
	}

	st := e.State()
	if st.Connection.Workspace != "customer-a" {
		t.Fatalf("workspace %q, want customer-a", st.Connection.Workspace)
	}
	if len(st.Hosts) != 0 || len(st.Services) != 0 || len(st.Creds) != 0 || len(st.Loot) != 0 {
		t.Fatalf("old workspace data survived a failed refresh: %d hosts, %d services, %d creds, %d loot",
			len(st.Hosts), len(st.Services), len(st.Creds), len(st.Loot))
	}

	// the clear must reach already-connected browsers, not just snapshots
	workspace := ""
	cleared := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(cleared) < 5 {
		select {
		case m := <-sub.C():
			r, ok := m.(protocol.ResourceUpdate)
			if !ok {
				continue
			}
			switch r.Resource {
			case protocol.ResConnection:
				cleared[r.Resource] = true
				workspace = r.Connection.Workspace
			case protocol.ResHosts:
				cleared[r.Resource] = true
				if len(r.Hosts) != 0 {
					t.Fatalf("hosts broadcast carried %d rows, want the clear", len(r.Hosts))
				}
			case protocol.ResServices, protocol.ResCreds, protocol.ResLoot:
				cleared[r.Resource] = true
			}
		case <-deadline:
			t.Fatalf("clear broadcasts incomplete: %v", cleared)
		}
	}
	if workspace != "customer-a" {
		t.Fatalf("connection broadcast carried workspace %q", workspace)
	}
}

// A periodic refresh that already read the old workspace must not be able
// to commit its stale rows after workspace.set cleared them. The switch
// transition holds refreshMu end to end, so even when the command's own
// reload then fails, the old workspace's data stays cleared instead of
// surviving through the interleaved stale commit.
func TestWorkspaceSetSerializesWithInFlightRefresh(t *testing.T) {
	fake := stdFake()
	var wsMu sync.Mutex
	current := "default"
	fake.set(gomsf.DbSetWorkspace, func(args ...interface{}) (interface{}, error) {
		wsMu.Lock()
		current = args[0].(string)
		wsMu.Unlock()
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.DbCurrentWorkspace, func(args ...interface{}) (interface{}, error) {
		wsMu.Lock()
		defer wsMu.Unlock()
		return map[string]interface{}{"workspace": current}, nil
	})
	// every DbHosts read after the bootstrap one captures its rows, then
	// parks until released: the read that started before the switch models
	// exactly the stale commit the clear must survive
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	reads := 0
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		reads++
		first := reads == 1
		wsMu.Lock()
		ws := current
		wsMu.Unlock()
		mu.Unlock()
		if !first {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
		}
		if ws == "default" {
			return map[string]interface{}{"hosts": []interface{}{
				map[string]interface{}{"host": "10.0.0.5", "address": "10.0.0.5", "os_name": "Linux"},
			}}, nil
		}
		return nil, fmt.Errorf("%w: reload failed", gomsf.ErrRPC)
	})

	e := connectedEngine(t, fake)
	if st := e.State(); len(st.Hosts) != 1 {
		t.Fatalf("setup: want 1 host before the switch, got %d", len(st.Hosts))
	}
	<-entered // the refresh loop's first sweep is parked inside its read

	done := make(chan *protocol.ErrorBody, 1)
	go func() {
		_, eb := e.Exec(context.Background(), "dana", protocol.MethodWorkspaceSet,
			json.RawMessage(`{"name":"customer-b"}`))
		done <- eb
	}()
	// the switch must queue behind the parked refresh instead of clearing
	// state the old read will then overwrite
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case eb := <-done:
		if eb == nil || eb.Code != protocol.CodeRPC {
			t.Fatalf("want the failed reload surfaced, got %+v", eb)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workspace.set never finished")
	}
	st := e.State()
	if st.Connection.Workspace != "customer-b" {
		t.Fatalf("workspace %q, want customer-b", st.Connection.Workspace)
	}
	if len(st.Hosts) != 0 {
		t.Fatalf("stale rows from the old workspace survived the switch: %d hosts", len(st.Hosts))
	}
}
