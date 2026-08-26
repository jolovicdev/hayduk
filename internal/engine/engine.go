// Package engine owns the campaign state: everything the operator sees is
// mirrored from here. The engine is the only reader of watched msfrpcd
// streams (console and attached session), per go-msf's ownership rule.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

const (
	eventRingCap   = 2000
	streamCap      = 64 << 10 // console/interact byte buffers
	errStreakLimit = 3        // consecutive monitor errors before reconnect
)

type Config struct {
	SessionInterval time.Duration
	JobInterval     time.Duration
	OutputInterval  time.Duration
	RefreshInterval time.Duration
	RouteInterval   time.Duration
	// RPC, when set, replaces the real client (tests). The engine then never
	// dials msfrpcd itself.
	RPC gomsf.RPCCaller
}

func (c Config) withDefaults() Config {
	if c.SessionInterval <= 0 {
		c.SessionInterval = time.Second
	}
	if c.JobInterval <= 0 {
		c.JobInterval = time.Second
	}
	if c.OutputInterval <= 0 {
		c.OutputInterval = 500 * time.Millisecond
	}
	if c.RefreshInterval <= 0 {
		c.RefreshInterval = 5 * time.Second
	}
	if c.RouteInterval <= 0 {
		c.RouteInterval = 10 * time.Second
	}
	return c
}

type Engine struct {
	cfg Config

	mu           sync.Mutex // guards everything below
	rpc          gomsf.RPCCaller
	password     string // retained for reconnect; never leaves memory
	lastPassword string // last successful connect password; reconnect only
	runCtx       context.Context
	runCancel    context.CancelFunc
	monitor      *gomsf.EventMonitor
	console      *gomsf.MsfConsole
	consoleID    string
	routeConsole *gomsf.MsfConsole

	conn          protocol.ConnectionState
	hosts         []*protocol.HostState
	services      []*protocol.ServiceState
	sessions      map[string]*protocol.SessionState
	jobs          map[string]*protocol.JobState
	routes        []*protocol.RouteState
	creds         []*protocol.CredState
	loot          []*protocol.LootState
	modules       *protocol.ModuleIndex
	moduleRanks   map[string]string
	consoleOut    []byte
	consolePrompt string
	interactSID   string
	interactOut   []byte
	events        []*protocol.EventEntry
	operators     map[string]int
	seq           int64
	errStreak     int

	connecting bool
	// refreshMu serializes db refreshes: the loop's periodic sweep and
	// command-driven ones can overlap, and a stalled read committing after
	// a newer refresh would revert it
	refreshMu sync.Mutex
	// gen counts connection attempts that took effect (commits, teardowns).
	// Async work started under one generation must not commit under a later
	// one: a disconnect or a newer connection invalidates it.
	gen uint64

	// routeKick wakes the route poller out of band; an autoroute job ending
	// kicks it so new pivots show up at once instead of next interval
	routeKick chan struct{}

	bus *bus
	wg  sync.WaitGroup
}

func New(cfg Config) *Engine {
	cfg = cfg.withDefaults()
	e := &Engine{
		cfg:       cfg,
		sessions:  make(map[string]*protocol.SessionState),
		jobs:      make(map[string]*protocol.JobState),
		bus:       newBus(),
		conn:      protocol.ConnectionState{Status: "disconnected"},
		routeKick: make(chan struct{}, 1),
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.bus.run()
	}()
	return e
}

// Shutdown stops background work. Call once; New's caller owns it.
func (e *Engine) Shutdown() {
	e.Disconnect()
	e.bus.shutdown()
	e.wg.Wait()
}

// Subscription delivers engine broadcasts until Stop. A closed channel means
// the consumer fell behind: reconnect and take a fresh snapshot.
type Subscription struct {
	ch   chan protocol.ServerMessage
	id   int
	bus  *bus
	once sync.Once
}

func (s *Subscription) C() <-chan protocol.ServerMessage { return s.ch }

func (s *Subscription) Stop() {
	s.once.Do(func() { s.bus.unsubscribe(s.id) })
}

func (e *Engine) Subscribe() *Subscription {
	id, ch := e.bus.subscribe()
	return &Subscription{ch: ch, id: id, bus: e.bus}
}

// State returns a deep copy safe to hand to another goroutine.
func (e *Engine) State() protocol.CampaignState {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := protocol.CampaignState{
		Connection:  e.conn,
		Modules:     e.modules,
		ModuleRanks: copyMap(e.moduleRanks),
	}
	if s.ModuleRanks == nil {
		s.ModuleRanks = map[string]string{}
	}
	// empty collections, never nil: the wire contract promises [] and {}
	// so browser stores can spread a snapshot without null guards
	s.Hosts = emptyIfNil(deepSlice(e.hosts))
	s.Services = emptyIfNil(deepSlice(e.services))
	s.Creds = emptyIfNil(deepSlice(e.creds))
	s.Loot = emptyIfNil(deepSlice(e.loot))
	s.Events = emptyIfNil(deepSlice(e.events))
	s.Routes = emptyIfNil(deepSlice(e.routes))
	s.Sessions = make(map[string]*protocol.SessionState, len(e.sessions))
	for k, v := range e.sessions {
		c := *v
		s.Sessions[k] = &c
	}
	s.Jobs = make(map[string]*protocol.JobState, len(e.jobs))
	for k, v := range e.jobs {
		c := *v
		s.Jobs[k] = &c
	}
	if e.consoleID != "" {
		s.Console = &protocol.ConsoleState{Output: string(e.consoleOut)}
	}
	if e.interactSID != "" {
		s.Interact = &protocol.InteractState{SID: e.interactSID, Output: string(e.interactOut)}
	}
	s.Operators = e.operatorListLocked()
	return s
}

func emptyIfNil[T any](in []*T) []*T {
	if in == nil {
		return []*T{}
	}
	return in
}

// deepSlice clones a slice of pointers so callers cannot reach engine memory.
func deepSlice[T any](in []*T) []*T {
	if in == nil {
		return nil
	}
	out := make([]*T, len(in))
	for i, v := range in {
		c := *v
		out[i] = &c
	}
	return out
}

// copySlice and copyMap copy only the container; elements are shared, which
// is safe for broadcast values under the engine's copy-on-write rule.
func copySlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func copyMap[K comparable, V any](in map[K]V) map[K]V {
	if in == nil {
		return nil
	}
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// logf appends an event entry and broadcasts it. Callers must hold e.mu.
func (e *Engine) logf(level, format string, args ...any) {
	e.logfOp("", level, format, args...)
}

// logfOp is logf with operator attribution (team mode). Callers must hold e.mu.
func (e *Engine) logfOp(operator, level, format string, args ...any) {
	e.seq++
	entry := &protocol.EventEntry{
		Seq:      e.seq,
		Time:     time.Now().UTC(),
		Level:    level,
		Operator: operator,
		Text:     fmt.Sprintf(format, args...),
	}
	e.events = append(e.events, entry)
	if len(e.events) > eventRingCap {
		e.events = e.events[len(e.events)-eventRingCap:]
	}
	e.bus.send(protocol.ServerMessageFromEvent(entry))
}

// eventf is logf without requiring the caller to hold e.mu.
func (e *Engine) eventf(level, format string, args ...any) {
	e.eventfOp("", level, format, args...)
}

func (e *Engine) eventfOp(operator, level, format string, args ...any) {
	e.mu.Lock()
	e.logfOp(operator, level, format, args...)
	e.mu.Unlock()
}

// OperatorJoin and OperatorLeave track connected operators (team mode). The
// operators resource fires when a name's connection count crosses zero.
func (e *Engine) OperatorJoin(name string)  { e.operatorDelta(name, 1) }
func (e *Engine) OperatorLeave(name string) { e.operatorDelta(name, -1) }

func (e *Engine) operatorDelta(name string, delta int) {
	if name == "" {
		return
	}
	e.mu.Lock()
	if e.operators == nil {
		e.operators = make(map[string]int)
	}
	next := e.operators[name] + delta
	changed := false
	switch {
	case next <= 0 && e.operators[name] > 0:
		delete(e.operators, name)
		e.logf(protocol.LevelInfo, "operator %s left", name)
		changed = true
	case next > 0 && e.operators[name] == 0:
		e.operators[name] = next
		e.logf(protocol.LevelInfo, "operator %s joined", name)
		changed = true
	default:
		if next > 0 {
			e.operators[name] = next
		}
	}
	list := e.operatorListLocked()
	e.mu.Unlock()
	if changed {
		e.bus.send(protocol.OperatorsUpdate(list))
	}
}

func (e *Engine) operatorListLocked() []string {
	out := make([]string, 0, len(e.operators))
	for name := range e.operators {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Exec dispatches one client command. operator names the acting operator in
// team mode; empty means the local single operator.
func (e *Engine) Exec(ctx context.Context, operator, method string, params json.RawMessage) (json.RawMessage, *protocol.ErrorBody) {
	switch method {
	case protocol.MethodConnect:
		var p protocol.ConnectParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Host == "" {
			return nil, badParam("host is required")
		}
		if !validPort(p.Port) {
			return nil, badParam("port must be between 1 and 65535")
		}
		if eb := e.Connect(ctx, p); eb != nil {
			return nil, eb
		}
		return mustJSON(e.snapshotConn()), nil
	case protocol.MethodDisconnect:
		e.Disconnect()
		return nil, nil
	case protocol.MethodReportHTML:
		html, eb := e.reportDocument()
		if eb != nil {
			return nil, eb
		}
		return mustJSON(protocol.ReportPayload{HTML: html}), nil
	default:
		return e.execCommand(ctx, operator, method, params)
	}
}

// parseParams unmarshals params, mapping decode failure to bad_params.
func parseParams(params json.RawMessage, dst any) *protocol.ErrorBody {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, dst); err != nil {
		return &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: err.Error()}
	}
	return nil
}

func badParam(msg string) *protocol.ErrorBody {
	return &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: msg}
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func requireModuleRef(typ, name string) *protocol.ErrorBody {
	if typ == "" || name == "" {
		return badParam("type and name are required")
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// connectedRPC returns the live RPC client in one lock acquisition so a
// disconnect racing the caller cannot split a nil-check from the use.
func (e *Engine) connectedRPC() gomsf.RPCCaller {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rpc
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func cleanOutput(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "\r", "")
}
