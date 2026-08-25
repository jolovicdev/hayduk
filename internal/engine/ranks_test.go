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
	if ranks["windows/smb/a"] != "excellent" || ranks["multi/http/c"] != "normal" {
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
