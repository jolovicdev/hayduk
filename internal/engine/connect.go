package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// release tears down a dropped msfrpcd session: every console destroyed and
// the RPC client logged out. Best effort with a short deadline - the daemon
// may already be gone or hanging, and cleanup must never wedge the caller.
func release(rpc gomsf.RPCCaller, consoleIDs []string, routeCID string) {
	if rpc == nil {
		return
	}
	releaseConsoles(rpc, append(consoleIDs, routeCID))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, ok := rpc.(*gomsf.Client); ok {
		_ = c.Logout(ctx)
	}
}

// Connect dials msfrpcd and bootstraps state. It is synchronous: it returns
// once the snapshot-worthy state is loaded (modules, db, console, monitor).
func (e *Engine) Connect(ctx context.Context, p protocol.ConnectParams) *protocol.ErrorBody {
	return e.ConnectAs(ctx, p, "")
}

// ConnectAs is Connect with operator attribution for the shared event log:
// in team mode the whole team rides the link one operator establishes.
func (e *Engine) ConnectAs(ctx context.Context, p protocol.ConnectParams, operator string) *protocol.ErrorBody {
	e.mu.Lock()
	if e.connecting || e.rpc != nil {
		e.mu.Unlock()
		return &protocol.ErrorBody{Code: protocol.CodeBusy, Message: "already connecting or connected"}
	}
	e.connecting = true
	// connectSeq names this attempt as the slot owner: a bootstrap canceled
	// by Disconnect may still be unwinding when the next connect starts, and
	// only the current owner may touch connecting/connectCancel
	e.connectSeq++
	attempt := e.connectSeq
	gen := e.gen
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	e.connectCancel = cancelAttempt
	e.conn = protocol.ConnectionState{
		Status: "connecting", Host: p.Host, Port: p.Port,
		SSL: p.SSL, Username: p.Username,
	}
	e.bus.send(protocol.ConnectionUpdate(e.conn))
	e.mu.Unlock()

	err := e.bootstrap(attemptCtx, p, gen)

	e.mu.Lock()
	if e.connectSeq == attempt {
		e.connecting = false
		e.connectCancel = nil
	}
	ownsState := e.gen == gen
	if err != nil {
		if ownsState { // a disconnect or newer connect already owns the state
			e.conn.Status = "disconnected"
			e.conn.Error = err.Error()
			e.bus.send(protocol.ConnectionUpdate(e.conn))
		}
		e.mu.Unlock()
		cancelAttempt()
		return &protocol.ErrorBody{Code: protocol.CodeConnectFailed, Message: err.Error()}
	}
	if e.gen != gen+1 { // bootstrap committed, then something tore the link down
		e.mu.Unlock()
		cancelAttempt()
		return &protocol.ErrorBody{Code: protocol.CodeConnectFailed, Message: "connection attempt superseded"}
	}
	e.conn.Status = "connected"
	e.password = p.Password
	e.lastPassword = p.Password
	e.errStreak = 0
	e.logfOp(operator, protocol.LevelSuccess, "connected to %s:%d (metasploit %s)", p.Host, p.Port, e.conn.MSFVersion)
	e.bus.send(protocol.ConnectionUpdate(e.conn))
	// bootstrap swapped the whole campaign; a snapshot is the only message
	// that carries modules and db state to already-connected browsers
	e.broadcastSnapshotsLocked()
	e.mu.Unlock()
	if e.cfg.Store != nil {
		// Remember the endpoint, never the password: the dialog asks for it
		// on every fresh browser.
		_, _ = e.cfg.Store.SaveProfile(protocol.ProfilePayload{
			Host: p.Host, Port: p.Port, SSL: p.SSL, Username: p.Username,
		})
	}
	cancelAttempt()
	return nil
}

// errSuperseded marks a connection attempt that lost its generation: a
// disconnect or a newer connect committed while it was still bootstrapping.
var errSuperseded = errors.New("connection attempt superseded")

func (e *Engine) snapshotConn() protocol.ConnectionState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.conn
}

func (e *Engine) dial(ctx context.Context, p protocol.ConnectParams) (gomsf.RPCCaller, error) {
	if e.cfg.RPC != nil {
		return e.cfg.RPC, nil // injected; auth handled by the fake's auth.login
	}
	return gomsf.NewClient(p.Password,
		gomsf.WithHost(p.Host), gomsf.WithPort(p.Port),
		gomsf.WithSSL(p.SSL), gomsf.WithUsername(p.Username))
}

// fetchModuleIndex loads the seven module lists concurrently. They are
// independent RPCs; all goroutines finish before an error is returned so a
// failed bootstrap never leaves a read in flight against the connection it is
// about to release.
func fetchModuleIndex(ctx context.Context, mm *gomsf.ModuleManager) (*protocol.ModuleIndex, error) {
	modules := &protocol.ModuleIndex{}
	errs := make(chan error, 7)
	var wg sync.WaitGroup

	load := func(dst *[]string, fetch func(context.Context) ([]string, error)) {
		defer wg.Done()
		names, err := fetch(ctx)
		if err != nil {
			errs <- err
			return
		}
		*dst = names
	}

	wg.Add(7)
	go load(&modules.Exploits, mm.Exploits)
	go load(&modules.Auxiliary, mm.Auxiliary)
	go load(&modules.Post, mm.Post)
	go load(&modules.Payloads, mm.Payloads)
	go load(&modules.Encoders, mm.Encoders)
	go load(&modules.Nops, mm.Nops)
	go load(&modules.Evasion, mm.Evasion)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return modules, nil
}

func (e *Engine) bootstrap(ctx context.Context, p protocol.ConnectParams, gen uint64) error {
	e.eventf(protocol.LevelInfo, "authenticating against %s:%d", p.Host, p.Port)
	rpc, err := e.dial(ctx, p)
	if err != nil {
		return err
	}
	// rpc is authenticated from here on: if this bootstrap does not commit,
	// its consoles and login must not linger on the daemon
	var conIDs []string
	var routeID string
	committed := false
	defer func() {
		if !committed {
			release(rpc, conIDs, routeID)
		}
	}()

	version, err := gomsf.NewCoreManager(rpc).Version(ctx)
	if err != nil {
		return err
	}
	e.eventf(protocol.LevelInfo, "metasploit %s, loading module list", version.Version)

	mm := gomsf.NewModuleManager(rpc)
	db := gomsf.NewDbManager(rpc)

	// The seven module lists and the workspace read are independent. Run
	// them together; the DB page collection still needs the workspace, so it
	// starts only after this barrier.
	var (
		modules   *protocol.ModuleIndex
		moduleErr error
		workspace string
		loadWg    sync.WaitGroup
	)
	loadWg.Add(2)
	go func() {
		defer loadWg.Done()
		modules, moduleErr = fetchModuleIndex(ctx, mm)
	}()
	go func() {
		defer loadWg.Done()
		workspace, _ = db.CurrentWorkspace(ctx) // "" when no db; not fatal
	}()
	loadWg.Wait()
	if moduleErr != nil {
		return moduleErr
	}

	// pages are pinned to the workspace read above for the same reason the
	// refresh pins its own: a console workspace switch mid-load must not mix
	// collections, and msf's unpaged reads truncate at 100 rows
	hosts, services, creds, loots, err := fetchWorkspaceCollections(ctx, db, workspace)
	if err != nil {
		return err
	}

	e.eventf(protocol.LevelInfo, "loaded %d modules, reading database", len(modules.Exploits)+len(modules.Auxiliary)+len(modules.Post)+len(modules.Payloads)+len(modules.Encoders)+len(modules.Nops)+len(modules.Evasion))

	// One private console per present operator (team mode); with nobody
	// joined yet the "" console serves the local single operator. Operators
	// who join mid-bootstrap get theirs from the post-commit sweep.
	e.mu.Lock()
	names := make([]string, 0, len(e.operators))
	for name := range e.operators {
		names = append(names, name)
	}
	e.mu.Unlock()
	if len(names) == 0 {
		names = []string{""}
	}
	sort.Strings(names)
	consoles := make(map[string]*operatorConsole, len(names))
	for _, name := range names {
		con, err := gomsf.NewConsoleManager(rpc).Create(ctx)
		if err != nil {
			return fmt.Errorf("create console: %w", err)
		}
		conIDs = append(conIDs, con.ID)
		consoles[name] = &operatorConsole{name: name, console: gomsf.NewMsfConsole(rpc, con.ID)}
	}
	// The route poll needs its own console so route tables never leak into
	// the operator's console stream. Failure is non-fatal: no routes tracked.
	var routeCon *gomsf.MsfConsole
	if con2, err := gomsf.NewConsoleManager(rpc).Create(ctx); err == nil {
		routeCon = gomsf.NewMsfConsole(rpc, con2.ID)
		routeID = con2.ID
	} else {
		e.eventf(protocol.LevelWarn, "route console unavailable; pivot routes will not be tracked")
	}
	e.eventf(protocol.LevelInfo, "console ready, %d hosts and %d services in workspace", len(hosts), len(services))

	// Seed the daemon's live sessions before the monitor's first sync can
	// report them as opened: sessions that already existed must not be
	// re-tagged with the workspace active at (re)connect. Known
	// attributions survive in sessionTags when the uuid proves the session
	// is the same one - ids are reused across daemons and restarts.
	// Sessions first seen now stay untagged and the report's host-membership
	// fallback decides. A failed list is non-fatal: the monitor then picks
	// them up tagged with the current workspace.
	liveSessions, _ := gomsf.NewSessionManager(rpc).List(ctx)
	// The job list seeds the listener registry reconciliation: a persisted
	// handler whose job the daemon no longer has must not survive.
	liveJobs, jobsErr := gomsf.NewJobManager(rpc).List(ctx)
	e.mu.Lock()
	prevTags := make(map[string]sessionTag, len(e.sessionTags))
	for sid, tag := range e.sessionTags {
		prevTags[sid] = tag
	}
	e.mu.Unlock()
	seedSessions := make(map[string]*protocol.SessionState, len(liveSessions))
	for sid, s := range liveSessions {
		tag := restoreTag(prevTags[sid], s.UUID)
		st := sessionState(sid, s, tag.workspace)
		st.OpenedBy = tag.operator
		seedSessions[sid] = st
	}

	runCtx, cancel := context.WithCancel(context.Background())
	monitor := gomsf.NewEventMonitor(runCtx, rpc,
		gomsf.WithEventSessionInterval(e.cfg.SessionInterval),
		gomsf.WithEventJobInterval(e.cfg.JobInterval),
		gomsf.WithEventOutputInterval(e.cfg.OutputInterval),
	)
	// Commit: swap in the fresh rpc handle and state, then start the loops.
	// A newer generation (disconnect or another connect that finished first)
	// discards this attempt instead of clobbering the newer state.
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		cancel()             // our monitor's ctx; nobody else owns it
		return errSuperseded // the defer releases this attempt's consoles
	}
	oldCancel := e.runCancel
	oldRPC := e.rpc
	oldConsoleIDs := make([]string, 0, len(e.consoles))
	for _, oc := range e.consoles {
		oldConsoleIDs = append(oldConsoleIDs, oc.console.CID)
		oc.cancel()
	}
	for _, t := range e.consoleTimers {
		t.Stop()
	}
	e.consoleTimers = nil
	oldRouteCID := ""
	if e.routeConsole != nil {
		oldRouteCID = e.routeConsole.CID
	}
	e.rpc = rpc
	e.runCtx = runCtx
	e.runCancel = cancel
	e.monitor = monitor
	for _, oc := range consoles {
		oc.loopCtx, oc.cancel = context.WithCancel(runCtx)
	}
	e.consoles = consoles
	e.routeConsole = routeCon
	e.routes = nil
	e.conn.MSFVersion = version.Version
	e.conn.Workspace = workspace
	e.conn.Error = ""
	e.modules = modules
	e.preparedAttacks = prepareAttacks(modules.Exploits)
	e.modInfoCache = nil
	e.modOptionsCache = nil
	e.modPayloadsCache = nil
	stagingDir, stagingAuto, stagingErr := resolveStaging(e.cfg.StagingDir, p.Host)
	if stagingErr != nil {
		e.eventf(protocol.LevelWarn, "file staging unavailable: %v", stagingErr)
	}
	e.stagingDir = stagingDir
	e.stagingAuto = stagingAuto
	e.conn.FileStaging = "remote"
	if stagingDir != "" {
		e.conn.FileStaging = "local"
	}
	e.hosts = hostStates(hosts)
	e.services = serviceStates(services)
	e.creds = credStates(creds)
	e.loot = lootStates(loots)
	e.sessions = seedSessions
	// attribution survives the reconnect; tags of sessions that died while
	// offline go with them
	for sid := range e.sessionTags {
		if _, live := seedSessions[sid]; !live {
			delete(e.sessionTags, sid)
		}
	}
	for sid, st := range seedSessions {
		if st.Workspace == "" && st.UUID == "" {
			continue
		}
		if e.sessionTags == nil {
			e.sessionTags = make(map[string]sessionTag)
		}
		e.sessionTags[sid] = sessionTag{workspace: st.Workspace, uuid: st.UUID, operator: st.OpenedBy}
	}
	e.jobs = make(map[string]*protocol.JobState, len(liveJobs))
	if jobsErr == nil {
		for id, name := range liveJobs {
			e.jobs[id] = &protocol.JobState{ID: id, Name: name, StartedAt: time.Now().UTC()}
		}
	}
	listenerPersist := e.seedListenersLocked(liveJobs, jobsErr == nil)
	e.exploitRuns = nil
	e.earlySessions = nil
	e.errStreak = 0
	e.gen = gen + 1
	// operators who joined while this bootstrap was loading still need
	// their private console on the new link
	var missing []string
	for name := range e.operators {
		if e.consoles[name] == nil {
			missing = append(missing, name)
		}
	}
	e.mu.Unlock()
	committed = true // the engine owns rpc and the consoles from the swap on
	if listenerPersist != nil {
		_ = e.cfg.Store.SaveListeners(listenerPersist)
	}
	if oldCancel != nil {
		oldCancel()
	}
	go release(oldRPC, oldConsoleIDs, oldRouteCID)

	if workspace == "" {
		e.eventf(protocol.LevelWarn, "msf database not connected; hosts and credentials views will stay empty")
	}

	go e.ingest(monitor, monitor.C())
	for _, oc := range consoles {
		go e.consoleLoop(oc.loopCtx, monitor, oc)
	}
	if routeCon != nil {
		go e.routeLoop(runCtx, monitor, routeCon)
	}
	go e.refreshLoop(runCtx)
	go e.rankPrefetch(runCtx, rpc)
	for _, name := range missing {
		go e.ensureConsole(runCtx, name)
	}
	return nil
}

// Disconnect drops the msfrpcd link. Discovered data (hosts, creds, events)
// is kept; live state (sessions, jobs, consoles) dies with the link and is
// rebuilt on next connect.
func (e *Engine) Disconnect() { e.DisconnectAs("") }

// DisconnectAs is Disconnect with operator attribution: in team mode one
// operator's disconnect drops the shared link for everyone, and the event
// log says who did it.
func (e *Engine) DisconnectAs(operator string) {
	e.mu.Lock()
	e.gen++ // invalidates any bootstrap or refresh still in flight
	if e.connecting {
		// Free the connection slot at once: the canceled bootstrap unwinds
		// on its own schedule and must not lock out a fresh connect. It
		// cannot clobber the next attempt either - slot ownership is by
		// connectSeq, and the old attempt fails on its canceled context.
		if e.connectCancel != nil {
			e.connectCancel()
		}
		e.connecting = false
	}
	cancel := e.runCancel
	// every in-flight operation belongs to the link being dropped; mark
	// them canceled now so the table is truthful even before the workers
	// unwind, then cancel their contexts below
	opCancels := e.cancelOperationsLocked()
	dropRPC := e.rpc
	dropConsoleIDs := make([]string, 0, len(e.consoles))
	for _, oc := range e.consoles {
		dropConsoleIDs = append(dropConsoleIDs, oc.console.CID)
	}
	for _, t := range e.consoleTimers {
		t.Stop()
	}
	e.consoleTimers = nil
	dropRouteCID := ""
	if e.routeConsole != nil {
		dropRouteCID = e.routeConsole.CID
	}
	e.runCancel = nil
	e.rpc = nil
	e.monitor = nil
	e.consoles = nil
	e.routeConsole = nil
	hadRoutes := len(e.routes) > 0
	e.routes = nil
	hadSessions := len(e.sessions) > 0
	hadJobs := len(e.jobs) > 0
	hadListeners := e.clearListenersLocked()
	e.sessions = make(map[string]*protocol.SessionState)
	e.jobs = make(map[string]*protocol.JobState)
	// every operator's session view dies with the link; each socket hears
	// its own interact clear
	for op := range e.interactActive {
		e.bus.send(protocol.InteractUpdate(op, &protocol.InteractState{}))
	}
	e.interactActive = nil
	e.attached = nil
	e.interactBufs = nil
	// Clear pending runs so their timers cannot warn after disconnect.
	e.exploitRuns = nil
	e.earlySessions = nil
	e.preparedAttacks = nil
	e.modInfoCache = nil
	e.modOptionsCache = nil
	e.modPayloadsCache = nil
	dropStaging := ""
	if e.stagingAuto {
		dropStaging = e.stagingDir
	}
	e.stagingDir = ""
	e.stagingAuto = false
	e.conn.FileStaging = ""
	if e.conn.Status != "disconnected" {
		e.conn.Status = "disconnected"
		e.conn.Error = ""
		if operator != "" {
			e.logfOp(operator, protocol.LevelWarn, "disconnected the team from %s:%d", e.conn.Host, e.conn.Port)
		} else {
			e.logf(protocol.LevelInfo, "disconnected from %s:%d", e.conn.Host, e.conn.Port)
		}
	}
	e.password = ""
	e.lastPassword = ""
	e.bus.send(protocol.ConnectionUpdate(e.conn))
	if hadRoutes {
		e.bus.send(protocol.RoutesUpdate([]*protocol.RouteState{}))
	}
	if hadSessions {
		e.bus.send(protocol.SessionsUpdate(map[string]*protocol.SessionState{}))
	}
	if hadJobs {
		e.bus.send(protocol.JobsUpdate(map[string]*protocol.JobState{}))
	}
	if hadListeners {
		e.bus.send(protocol.ListenersUpdate([]*protocol.ListenerState{}))
	}
	e.mu.Unlock()
	for _, opCancel := range opCancels {
		opCancel()
	}
	if cancel != nil {
		cancel()
	}
	// consoles destroyed and the client logged out only after the operator
	// saw the disconnect; bounded so a dead daemon cannot wedge this call
	release(dropRPC, dropConsoleIDs, dropRouteCID)
	if dropStaging != "" {
		_ = os.RemoveAll(dropStaging)
	}
}

func hostStates(in []*gomsf.Host) []*protocol.HostState {
	out := make([]*protocol.HostState, 0, len(in))
	for _, h := range in {
		out = append(out, &protocol.HostState{
			Address: h.Address, Name: h.Name, Mac: h.Mac,
			OSName: h.OSName, OSFlavor: h.OSFlavor, OSVersion: h.OSVersion,
			Purpose: h.Purpose, Info: h.Info, Comments: h.Comments,
		})
	}
	return out
}

func serviceStates(in []*gomsf.Service) []*protocol.ServiceState {
	out := make([]*protocol.ServiceState, 0, len(in))
	for _, s := range in {
		out = append(out, &protocol.ServiceState{
			Host: s.Host, Port: s.Port, Proto: s.Proto,
			Name: s.Name, State: s.State, Info: s.Info,
		})
	}
	return out
}

func credStates(in []*gomsf.Credential) []*protocol.CredState {
	out := make([]*protocol.CredState, 0, len(in))
	for _, c := range in {
		out = append(out, &protocol.CredState{
			Host: c.Host, Port: c.Port, Proto: c.Proto, Service: c.Service,
			User: c.User, Pass: c.Pass, Type: c.Type,
		})
	}
	return out
}

func lootStates(in []*gomsf.Loot) []*protocol.LootState {
	out := make([]*protocol.LootState, 0, len(in))
	for _, l := range in {
		out = append(out, &protocol.LootState{Host: l.Host, Type: l.Type, Name: l.Name, Info: l.Info})
	}
	return out
}
