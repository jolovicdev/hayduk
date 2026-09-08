package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestRankPrefetchBatchesAndCaches(t *testing.T) {
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules",
		"windows/smb/a", "windows/smb/b", "multi/http/c")
	fake.setList(gomsf.ModuleAuxiliary, "modules", "scanner/smb/d")
	fake.setList(gomsf.ModulePost, "modules", "multi/manage/e")
	fake.set(gomsf.ModuleInfo, func(args ...interface{}) (interface{}, error) {
		name := args[1].(string)
		rank := "normal"
		if name == "windows/smb/a" || name == "scanner/smb/d" {
			rank = "excellent"
		}
		return map[string]interface{}{"name": name, "rank": rank}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	waitFor(t, func() bool { return len(e.State().ModuleRanks) == 5 })
	ranks := e.State().ModuleRanks
	if ranks["exploit/windows/smb/a"] != "excellent" || ranks["exploit/multi/http/c"] != "normal" {
		t.Fatalf("ranks %+v", ranks)
	}

	sawBatch := false
	deadline := time.After(time.Second)
	for !sawBatch {
		select {
		case m := <-sub.C():
			if up, ok := m.(protocol.ResourceUpdate); ok && up.Resource == protocol.ResModuleRanks {
				sawBatch = true
			}
		case <-deadline:
			t.Fatal("no moduleRanks resource update broadcast")
		}
	}

	// a second connect reuses the cache instead of re-crawling
	e.Disconnect()
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("reconnect: %+v", err)
	}
	waitFor(t, func() bool { return len(e.State().ModuleRanks) == 5 })
}

// Rank keys are namespaced by module type: exploit and auxiliary catalogs
// can contain the same refname.
func TestRankCacheSeparatesModuleTypes(t *testing.T) {
	e := testEngine(t)
	e.modules = &protocol.ModuleIndex{Exploits: []string{"test/shared"}, Auxiliary: []string{"test/shared"}}
	f := stdFake()
	f.set(gomsf.ModuleInfo, func(args ...interface{}) (interface{}, error) {
		rank := "manual"
		if args[0].(string) == "exploit" {
			rank = "excellent"
		}
		return map[string]interface{}{"name": args[1], "rank": rank}, nil
	})
	e.rankPrefetch(context.Background(), f)
	got := e.State().ModuleRanks
	if len(got) != 2 || got["exploit/test/shared"] != "excellent" || got["auxiliary/test/shared"] != "manual" {
		t.Fatalf("ranks %+v, want separate per-type entries", got)
	}
}

// A stale prefetch must not commit over the replacement connection's cache.
func TestCancelledRankPrefetchCannotCommit(t *testing.T) {
	e := testEngine(t)
	e.modules = &protocol.ModuleIndex{Exploits: []string{"test/stale"}}
	f := stdFake()
	entered, resume := make(chan struct{}), make(chan struct{})
	f.set(gomsf.ModuleInfo, func(...interface{}) (interface{}, error) {
		close(entered)
		<-resume
		return map[string]interface{}{"name": "stale", "rank": "manual"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.rankPrefetch(ctx, f); close(done) }()
	<-entered
	cancel()
	e.mu.Lock()
	e.gen++
	e.moduleRanks = map[string]string{"exploit/test/stale": "excellent"}
	e.mu.Unlock()
	close(resume)
	<-done
	if got := e.State().ModuleRanks["exploit/test/stale"]; got != "excellent" {
		t.Fatalf("stale prefetch overwrote the rank: got %q", got)
	}
}
