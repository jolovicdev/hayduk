package engine

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/jolovicdev/go-msf/v2"
)

// fakeRPC scripts RPC responses per method. Handlers may be swapped at any
// time from tests; later calls see the new behavior.
type fakeRPC struct {
	mu       sync.Mutex
	handlers map[gomsf.MsfRpcMethod]func(args ...interface{}) (interface{}, error)
}

func (f *fakeRPC) Call(_ context.Context, method gomsf.MsfRpcMethod, args ...interface{}) (interface{}, error) {
	f.mu.Lock()
	h, ok := f.handlers[method]
	f.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("fakeRPC: no handler for %s", method)
	}
	return h(args...)
}

func (f *fakeRPC) set(method gomsf.MsfRpcMethod, h func(args ...interface{}) (interface{}, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.handlers == nil {
		f.handlers = make(map[gomsf.MsfRpcMethod]func(args ...interface{}) (interface{}, error))
	}
	f.handlers[method] = h
}

func (f *fakeRPC) setList(method gomsf.MsfRpcMethod, key string, items ...string) {
	f.set(method, func(args ...interface{}) (interface{}, error) {
		list := make([]interface{}, len(items))
		for i, it := range items {
			list[i] = it
		}
		return map[string]interface{}{key: list}, nil
	})
}

func (f *fakeRPC) setError(method gomsf.MsfRpcMethod, err error) {
	f.set(method, func(args ...interface{}) (interface{}, error) {
		return nil, err
	})
}

// stdFake answers the bootstrap calls every connect test needs.
func stdFake() *fakeRPC {
	f := &fakeRPC{}
	f.set(gomsf.AuthLogin, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"result": "success", "token": "tok"}, nil
	})
	f.set(gomsf.CoreVersion, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"version": "6.5.2", "ruby": "3.3", "api": "1.0"}, nil
	})
	f.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	f.set(gomsf.JobList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	f.set(gomsf.DbCurrentWorkspace, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"workspace": "default"}, nil
	})
	f.setList(gomsf.ModuleExploits, "modules")
	f.setList(gomsf.ModuleAuxiliary, "modules")
	f.setList(gomsf.ModulePost, "modules")
	f.setList(gomsf.ModulePayloads, "modules")
	f.setList(gomsf.ModuleEncoders, "modules")
	f.setList(gomsf.ModuleNops, "modules")
	f.setList(gomsf.ModuleEvasion, "modules")
	f.set(gomsf.DbHosts, okList("hosts"))
	f.set(gomsf.DbServices, okList("services"))
	f.set(gomsf.DbCreds, okList("creds"))
	f.set(gomsf.DbLoots, okList("loots"))
	var consoles int32
	f.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		id := atomic.AddInt32(&consoles, 1)
		return map[string]interface{}{"id": strconv.Itoa(int(id - 1))}, nil
	})
	f.set(gomsf.ConsoleWrite, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"wrote": true}, nil
	})
	// quiet console stream unless a test scripts one; without a handler the
	// console loop's errors would trip the monitor's reconnect logic
	f.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})
	return f
}
