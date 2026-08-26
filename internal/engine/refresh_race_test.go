package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Two refreshes can be in flight at once: the loop's periodic one and a
// command-driven one (db.refresh, workspace.set). A stalled refresh that
// read older data must not commit after a newer one and revert it.
func TestStalledRefreshCannotRevertNewerState(t *testing.T) {
	host := func(os string) map[string]interface{} {
		return map[string]interface{}{"hosts": []interface{}{map[string]interface{}{
			"host": "10.0.0.5", "address": "10.0.0.5", "os_name": os,
		}}}
	}
	fake := stdFake()
	// bootstrap reads hosts once (call 1); the refresh loop's initial sweep
	// is call 2, parked mid-read so a newer refresh can lap it
	var calls atomic.Int32
	parked := make(chan struct{})
	release := make(chan struct{})
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		if calls.Add(1) == 2 {
			close(parked)
			<-release
		}
		return host("Linux"), nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	t.Cleanup(sub.Stop)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop's initial refresh never started")
	}

	// the data ripens while the loop's sweep is stalled mid-read
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return host("Ubuntu 22.04"), nil
	})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.refreshDB(context.Background())
	}()
	time.Sleep(100 * time.Millisecond) // let the newer refresh read first
	close(release)                     // the stalled sweep resumes with its stale read
	wg.Wait()

	// exactly one hosts broadcast belongs here: the newer refresh's
	hosts := 0
	deadline := time.After(2 * time.Second)
	for hosts == 0 {
		select {
		case m := <-sub.C():
			if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResHosts {
				hosts++
			}
		case <-deadline:
			t.Fatal("the newer refresh never broadcast")
		}
	}
	select {
	case m := <-sub.C():
		if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResHosts {
			t.Fatalf("stalled refresh broadcast after the newer one: %+v", up.Hosts)
		}
	case <-time.After(250 * time.Millisecond):
	}
	if got := e.State().Hosts[0].OSName; got != "Ubuntu 22.04" {
		t.Fatalf("stalled refresh reverted the newer state: osName %q", got)
	}
}
