package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

// The four database collections are independent RPCs. Fetch them together
// instead of paying their latency one after another.
func TestRefreshFetchesCollectionsConcurrently(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	fake := stdFake()
	for _, table := range []struct {
		method gomsf.MsfRpcMethod
		key    string
	}{
		{gomsf.DbHosts, "hosts"}, {gomsf.DbServices, "services"},
		{gomsf.DbCreds, "creds"}, {gomsf.DbLoots, "loots"},
	} {
		table := table
		fake.set(table.method, func(args ...interface{}) (interface{}, error) {
			started <- table.key
			<-release
			return map[string]interface{}{table.key: []interface{}{}}, nil
		})
	}
	e := testEngine(t)
	e.rpc = fake
	e.conn.Workspace = "default"
	e.gen = 1

	done := make(chan error, 1)
	go func() { done <- e.refreshDB(context.Background()) }()
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("refresh started collections one at a time")
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent refresh did not finish")
	}
}

// A collection of exactly dbMaxRows answers every page full and then one
// empty page. The walk must ask for that final page instead of reporting
// the collection as over the cap.
func TestFetchAllPagesExactlyAtCap(t *testing.T) {
	fetch := func(_ context.Context, opts map[string]interface{}) ([]*int, error) {
		offset := opts["offset"].(int)
		if offset >= dbMaxRows {
			return nil, nil
		}
		rows := make([]*int, dbPageSize)
		for i := range rows {
			v := offset + i
			rows[i] = &v
		}
		return rows, nil
	}
	got, err := fetchAllPages(context.Background(), "default", fetch)
	if err != nil {
		t.Fatalf("exactly-at-cap collection rejected: %v", err)
	}
	if len(got) != dbMaxRows {
		t.Fatalf("got %d rows, want %d", len(got), dbMaxRows)
	}
}
