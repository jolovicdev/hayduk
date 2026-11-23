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
	"github.com/jolovicdev/hayduk/internal/store"
)

const (
	eventRingCap   = 2000
	streamCap      = 64 << 10 // console/interact byte buffers
	errStreakLimit = 3        // consecutive monitor errors before reconnect
	// consoleLinger keeps a departed operator's console and session
	// attachments alive briefly: a page refresh or short network blip
	// rejoining under the same name resumes the same transcript instead
	// of starting over. Presence itself drops at once.
	consoleLinger = 30 * time.Second
	// chatMaxLen bounds one team chat message.
	chatMaxLen = 2000
)

type Config struct {
	SessionInterval time.Duration
	JobInterval     time.Duration
	OutputInterval  time.Duration
	RefreshInterval time.Duration
	RouteInterval   time.Duration
	// ConsoleLinger is how long a departed operator's console and session
	// attachments survive for a rejoin under the same name; tests shrink it.
	ConsoleLinger time.Duration
	// RPC, when set, replaces the real client (tests). The engine then never
	// dials msfrpcd itself.
	RPC gomsf.RPCCaller
	// Store, when set, persists the event/audit log, the rank cache, the
	// listener registry and saved connection profiles.
	Store *store.Store
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
	if c.ConsoleLinger <= 0 {
		c.ConsoleLinger = consoleLinger
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
	routeConsole *gomsf.MsfConsole
	// consoles holds one private msf console per operator, Armitage-style:
	// team members never share a console stream. The "" key is the local
	// single-operator console. consoleTimers holds the linger timer of
	// each departed operator whose console has not been dropped yet.
	consoles      map[string]*operatorConsole
	consoleTimers map[string]*time.Timer

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
	// interactActive names each operator's active session console, and
	// attached tracks every session an operator holds open. Both are
	// per-operator so one team member's attach or detach never moves
	// another's view. interactBufs holds one shared transcript per
	// attached session: everyone viewing a session sees the same history
	// and live output, and switching the active tab erases nothing.
	interactActive map[string]string
	attached       map[string]map[string]bool
	interactBufs   map[string][]byte
	// sessionTags remembers the workspace each live session was opened
	// under, keyed by session id and validated by uuid: ids are per-daemon
	// counters and get reused, so attribution carries across a reconnect
	// only when the session is the same one. Survives disconnects.
	sessionTags map[string]sessionTag
	// exploitRuns is keyed by daemon job id or a synthetic inline key.
	// earlySessions holds sessions awaiting a run's execution UUID.
	// Both are cleared on disconnect.
	exploitRuns   map[string]*exploitRun
	earlySessions []earlySession
	events        []*protocol.EventEntry
	operators     map[string]int
	seq           int64
	errStreak     int
	// operations are the engine-owned long-running tasks: scans, login
	// sprays and Hail Marys. opSeq names them.
	operations map[string]*operation
	opSeq      uint64
	// listeners is the multi/handler registry, reconciled against the live
	// job list on every connect and persisted so it survives a restart.
	listeners []*protocol.ListenerState

	connecting bool
	// connectSeq names the connection attempt that owns the connecting slot;
	// connectCancel kills a bootstrap still unwinding after Disconnect freed
	// the slot for the next attempt. Both guarded by mu.
	connectSeq    uint64
	connectCancel context.CancelFunc
	// refreshMu serializes db refreshes: the loop's periodic sweep and
	// command-driven ones can overlap, and a stalled read committing after
	// a newer refresh would revert it
	refreshMu sync.Mutex
	// toolsMu serializes one-shot session commands and utility consoles:
	// both briefly own a stream the monitor also reads.
	toolsMu sync.Mutex
	// consoleMu serializes operator console creation so two joins for one
	// name cannot race into two daemon consoles.
	consoleMu sync.Mutex
	// gen counts connection attempts that took effect (commits, teardowns).
	// Async work started under one generation must not commit under a later
	// one: a disconnect or a newer connection invalidates it.
	gen uint64

	// routeKick wakes the route poller out of band; an autoroute job ending
	// kicks it so new pivots show up at once instead of next interval
	routeKick chan struct{}
	// refreshKick coalesces post-mutation refreshes. A command returns as
	// soon as the write lands; the table update follows on the refresh loop.
	refreshKick chan struct{}

	bus *bus
	wg  sync.WaitGroup
}

func New(cfg Config) *Engine {
	cfg = cfg.withDefaults()
	e := &Engine{
		cfg:         cfg,
		sessions:    make(map[string]*protocol.SessionState),
		jobs:        make(map[string]*protocol.JobState),
		bus:         newBus(),
		conn:        protocol.ConnectionState{Status: "disconnected"},
		routeKick:   make(chan struct{}, 1),
		refreshKick: make(chan struct{}, 1),
	}
	if cfg.Store != nil {
		e.moduleRanks = cfg.Store.LoadRanks()
		if events, err := cfg.Store.LoadEvents(eventRingCap); err == nil && len(events) > 0 {
			e.events = events
			// the audit log's sequence survives restarts: a reconnecting
			// browser must never see it jump backwards
			e.seq = events[len(events)-1].Seq
		}
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

// SubscribeSnapshot subscribes and takes the state snapshot inside one
// engine-lock hold, which is the atomic handshake a reconnecting client
// needs: every broadcast already reflected in the returned state is
// suppressed for the new subscriber (the bus skips messages queued up to
// the subscription point), and every later broadcast is delivered, so no
// update that lands mid-handshake can be lost and none that predates the
// snapshot can be replayed over it.
//
// The guarantee rests on senders broadcasting while still holding e.mu: the
// bus cutoff read here then cannot fall between a state change and its
// message.
func (e *Engine) SubscribeSnapshot() (*Subscription, protocol.CampaignState) {
	return e.SubscribeSnapshotFor("")
}

// SubscribeSnapshotFor is SubscribeSnapshot with the personal resources
// (console, interact) of one operator: a team socket subscribes under its
// operator name once it has joined.
func (e *Engine) SubscribeSnapshotFor(operator string) (*Subscription, protocol.CampaignState) {
	e.mu.Lock()
	id, ch := e.bus.subscribe()
	s := e.stateForLocked(operator)
	e.mu.Unlock()
	return &Subscription{ch: ch, id: id, bus: e.bus}, s
}

// ConsoleFor and InteractFor return one operator's personal resources, for
// the server to hand a socket that just joined or renamed.
func (e *Engine) ConsoleFor(operator string) *protocol.ConsoleState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.consoleStateForLocked(operator)
}

func (e *Engine) InteractFor(operator string) *protocol.InteractState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interactStateForLocked(operator)
}

func (e *Engine) consoleStateForLocked(operator string) *protocol.ConsoleState {
	oc := e.consoles[operator]
	if oc == nil {
		return nil
	}
	return &protocol.ConsoleState{Output: string(oc.out)}
}

func (e *Engine) interactStateForLocked(operator string) *protocol.InteractState {
	sid := e.interactActive[operator]
	if sid == "" {
		return nil
	}
	return &protocol.InteractState{SID: sid, Output: string(e.interactBufs[sid])}
}

// State returns a deep copy safe to hand to another goroutine.
func (e *Engine) State() protocol.CampaignState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateLocked()
}

// stateLocked is State for callers already holding e.mu: the local
// single-operator view.
func (e *Engine) stateLocked() protocol.CampaignState {
	return e.stateForLocked("")
}

// stateForLocked is the snapshot view of one operator: shared campaign
// resources plus that operator's personal console and interact state.
func (e *Engine) stateForLocked(operator string) protocol.CampaignState {
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
	s.Console = e.consoleStateForLocked(operator)
	s.Interact = e.interactStateForLocked(operator)
	s.Operators = e.operatorListLocked()
	s.Operations = e.operationListLocked()
	s.Listeners = emptyIfNil(deepSlice(e.listeners))
	return s
}

// broadcastSnapshotsLocked swaps the whole campaign into every browser: one
// snapshot per view, the local "" view plus each present operator's personal
// view. Sockets keep only their own. Callers hold e.mu.
func (e *Engine) broadcastSnapshotsLocked() {
	e.bus.send(protocol.NewSnapshot(e.stateForLocked("")))
	for name := range e.operators {
		e.bus.send(protocol.NewSnapshotFor(name, e.stateForLocked(name)))
	}
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

// copyMap copies only the container; elements are shared, which is safe for
// broadcast values under the engine's copy-on-write rule.
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
	e.logfIn(e.conn.Workspace, operator, level, format, args...)
}

// logfIn is logfOp with an explicit originating workspace: a command that
// started in one workspace keeps its events there even when the operator
// switches before the command's RPC returns. Callers must hold e.mu.
func (e *Engine) logfIn(ws, operator, level, format string, args ...any) {
	e.seq++
	entry := &protocol.EventEntry{
		Seq:      e.seq,
		Time:     time.Now().UTC(),
		Level:    level,
		Operator: operator,
		// the workspace the event belongs to; empty for framework-wide
		// events. Reports filter on it.
		Workspace: ws,
		Text:      fmt.Sprintf(format, args...),
	}
	e.events = append(e.events, entry)
	if len(e.events) > eventRingCap {
		e.events = e.events[len(e.events)-eventRingCap:]
	}
	if e.cfg.Store != nil {
		e.cfg.Store.AppendEvent(entry)
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

// eventfOpIn is eventfOp with an explicit originating workspace, for command
// results that land after RPC round trips.
func (e *Engine) eventfOpIn(ws, operator, level, format string, args ...any) {
	e.mu.Lock()
	e.logfIn(ws, operator, level, format, args...)
	e.mu.Unlock()
}

// OperatorJoin and OperatorLeave track connected operators (team mode). The
// operators resource fires when a name's connection count crosses zero.
// Presence owns the operator's personal resources: the first connection
// under a name gets a private msf console, and the last one's departure
// schedules its console and session attachments for release after a linger.
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
	becamePresent, becameAbsent := false, false
	switch {
	case next <= 0 && e.operators[name] > 0:
		delete(e.operators, name)
		e.logf(protocol.LevelInfo, "operator %s left", name)
		changed = true
		becameAbsent = true
	case next > 0 && e.operators[name] == 0:
		e.operators[name] = next
		e.logf(protocol.LevelInfo, "operator %s joined", name)
		changed = true
		becamePresent = true
	default:
		if next > 0 {
			e.operators[name] = next
		}
	}
	if becamePresent {
		// a rejoin inside the linger resumes the kept console
		if t := e.consoleTimers[name]; t != nil {
			t.Stop()
			delete(e.consoleTimers, name)
		}
	}
	list := e.operatorListLocked()
	if changed {
		e.bus.send(protocol.OperatorsUpdate(list))
	}
	rpc, runCtx := e.rpc, e.runCtx
	e.mu.Unlock()
	if becamePresent && rpc != nil {
		e.ensureConsole(runCtx, name)
	}
	if becameAbsent {
		e.scheduleConsoleDrop(name)
	}
}

// scheduleConsoleDrop arms the linger timer for a departed operator. The
// timer identity is recorded so a rejoin-and-leave cycle cannot have an
// older timer drop the newer stay.
func (e *Engine) scheduleConsoleDrop(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.operators[name] > 0 {
		return // already rejoined
	}
	var timer *time.Timer
	timer = time.AfterFunc(e.cfg.ConsoleLinger, func() { e.dropOperatorConsole(name, timer) })
	if e.consoleTimers == nil {
		e.consoleTimers = make(map[string]*time.Timer)
	}
	if old := e.consoleTimers[name]; old != nil {
		old.Stop()
	}
	e.consoleTimers[name] = timer
}

// dropOperatorConsole releases a departed operator's console and session
// attachments once the linger expired. Shared transcripts and watches
// survive while another operator still holds the session.
func (e *Engine) dropOperatorConsole(name string, timer *time.Timer) {
	e.mu.Lock()
	if e.consoleTimers[name] != timer || e.operators[name] > 0 {
		e.mu.Unlock()
		return
	}
	delete(e.consoleTimers, name)
	oc := e.consoles[name]
	delete(e.consoles, name)
	cid := ""
	if oc != nil {
		oc.cancel()
		cid = oc.console.CID
	}
	unwatch := e.detachOperatorLocked(name)
	rpc, mon := e.rpc, e.monitor
	e.mu.Unlock()
	if cid != "" && rpc != nil {
		go releaseConsoles(rpc, []string{cid})
	}
	if mon != nil {
		for _, sid := range unwatch {
			mon.UnwatchSession(sid)
		}
	}
}

// detachOperatorLocked removes every session attachment of one operator
// and returns the sessions nobody holds anymore (their transcripts were
// dropped and their watches must end). Callers hold e.mu.
func (e *Engine) detachOperatorLocked(name string) []string {
	var unwatch []string
	for sid := range e.attached[name] {
		if !e.sessionHeldByOtherLocked(name, sid) {
			delete(e.interactBufs, sid)
			unwatch = append(unwatch, sid)
		}
	}
	delete(e.attached, name)
	delete(e.interactActive, name)
	return unwatch
}

// sessionHeldByOtherLocked reports whether any operator other than name
// still holds sid open. Callers hold e.mu.
func (e *Engine) sessionHeldByOtherLocked(name, sid string) bool {
	for op, sids := range e.attached {
		if op != name && sids[sid] {
			return true
		}
	}
	return false
}

// sessionHeldLocked reports whether any operator holds sid open.
func (e *Engine) sessionHeldLocked(sid string) bool {
	for _, sids := range e.attached {
		if sids[sid] {
			return true
		}
	}
	return false
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
		if eb := e.ConnectAs(ctx, p, operator); eb != nil {
			return nil, eb
		}
		return mustJSON(e.snapshotConn()), nil
	case protocol.MethodDisconnect:
		e.DisconnectAs(operator)
		return nil, nil
	case protocol.MethodTeamChat:
		// chat is team coordination, not framework work: it flows even
		// while the msfrpcd link is down
		var p protocol.TeamChatParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return nil, badParam("text is required")
		}
		if len([]rune(text)) > chatMaxLen {
			return nil, badParam("message too long")
		}
		if operator == "" {
			return nil, badParam("chat needs an operator name")
		}
		e.eventfOpIn("", operator, protocol.LevelChat, "%s", text)
		return nil, nil
	case protocol.MethodProfileList:
		return mustJSON(e.profiles()), nil
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

// sessionTag is the remembered attribution of one session id.
type sessionTag struct {
	workspace string
	uuid      string
	operator  string
}

// restoreTag returns the remembered attribution when the uuid matches the
// live session's. A reused id (different uuid) or a missing uuid on either
// side restores nothing: the host-membership fallback decides.
func restoreTag(tag sessionTag, uuid string) sessionTag {
	if tag.uuid == "" || uuid == "" || tag.uuid != uuid {
		return sessionTag{}
	}
	return tag
}

// dispatchScope captures the rpc handle together with the workspace a command
// starts in: the workspace rides with the command's events so a result
// landing after a workspace switch cannot be re-attributed to the new one.
func (e *Engine) dispatchScope() (gomsf.RPCCaller, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rpc, e.conn.Workspace
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func cleanOutput(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "\r", "")
}
