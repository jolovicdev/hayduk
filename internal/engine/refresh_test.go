package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jolovicdev/go-msf/v2"
)

// The error streak counts consecutive failures; a successful db round trip
// resets it.
func TestSuccessfulRefreshClearsMonitorErrorStreak(t *testing.T) {
	e := connectedEngine(t, stdFake())

	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.monitorError(mon, errors.New("transient blip one"))
	e.monitorError(mon, errors.New("transient blip two"))

	if err := e.refreshDB(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	e.mu.Lock()
	streak := e.errStreak
	e.mu.Unlock()
	if streak != 0 {
		t.Fatalf("a successful db round trip must clear the error streak, got %d", streak)
	}
}

// msf caps unpaged db.* reads at 100 rows; the refresh walks the pages.
func TestRefreshFetchesAllDatabasePages(t *testing.T) {
	e := testEngine(t)
	f := stdFake()
	e.rpc = f
	for _, table := range []struct {
		method gomsf.MsfRpcMethod
		key    string
	}{
		{gomsf.DbHosts, "hosts"}, {gomsf.DbServices, "services"}, {gomsf.DbCreds, "creds"}, {gomsf.DbLoots, "loots"},
	} {
		f.set(table.method, func(args ...interface{}) (interface{}, error) {
			opts := args[0].(map[string]interface{})
			limit, offset := 100, 0
			if v, ok := opts["limit"].(int); ok {
				limit = v
			}
			if v, ok := opts["offset"].(int); ok {
				offset = v
			}
			rows := []interface{}{}
			for i := offset; i < 105 && i < offset+limit; i++ {
				rows = append(rows, map[string]interface{}{
					"address": fmt.Sprintf("10.0.0.%d", i+1), "host": "10.0.0.1", "name": fmt.Sprintf("row-%d", i),
				})
			}
			return map[string]interface{}{table.key: rows}, nil
		})
	}
	if err := e.refreshDB(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := e.State()
	for name, n := range map[string]int{"hosts": len(s.Hosts), "services": len(s.Services), "creds": len(s.Creds), "loot": len(s.Loot)} {
		if n != 105 {
			t.Errorf("%s: 105 rows in the database, %d in the campaign", name, n)
		}
	}
}
