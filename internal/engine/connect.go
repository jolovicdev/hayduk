package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// release tears down a dropped msfrpcd session: both consoles destroyed and
// the RPC client logged out. Best effort with a short deadline - the daemon
// may already be gone or hanging, and cleanup must never wedge the caller.
func release(rpc gomsf.RPCCaller, consoleID, routeCID string) {
	if rpc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	consoles := gomsf.NewConsoleManager(rpc)
	if consoleID != "" {
		_ = consoles.Destroy(ctx, consoleID)
	}
	if routeCID != "" {
		_ = consoles.Destroy(ctx, routeCID)
	}
	if c, ok := rpc.(*gomsf.Client); ok {
		_ = c.Logout(ctx)
	}
}

// Connect dials msfrpcd and bootstraps state. It is synchronous: it returns
// once the snapshot-worthy state is loaded (modules, db, console, monitor).
func (e *Engine) Connect(ctx context.Context, p protocol.ConnectParams) *protocol.ErrorBody {
	e.mu.Lock()
	if e.connecting || e.rpc != nil {
		e.mu.Unlock()
		return &protocol.ErrorBody{Code: protocol.CodeBusy, Message: "already connecting or connected"}
	}
	e.connecting = true
	gen := e.gen
	e.conn = protocol.ConnectionState{
		Status: "connecting", Host: p.Host, Port: p.Port,
		SSL: p.SSL, Username: p.Username,
	}
	e.mu.Unlock()
	e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))

	err := e.bootstrap(ctx, p, gen)

	e.mu.Lock()
	e.connecting = false
	ownsState := e.gen == gen
	if err != nil {
		if ownsState { // a disconnect or newer connect already owns the state
			e.conn.Status = "disconnected"
			e.conn.Error = err.Error()
		}
		e.mu.Unlock()
		if ownsState {
			e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))
		}
		return &protocol.ErrorBody{Code: protocol.CodeConnectFailed, Message: err.Error()}
	}
	if e.gen != gen+1 { // bootstrap committed, then something tore the link down
		e.mu.Unlock()
		return &protocol.ErrorBody{Code: protocol.CodeConnectFailed, Message: "connection attempt superseded"}
	}
	e.conn.Status = "connected"
	e.password = p.Password
	e.lastPassword = p.Password
	e.errStreak = 0
	e.logf(protocol.LevelSuccess, "connected to %s:%d (metasploit %s)", p.Host, p.Port, e.conn.MSFVersion)
	e.mu.Unlock()
	e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))
	// bootstrap swapped the whole campaign; a snapshot is the only message
	// that carries modules and db state to already-connected browsers
	e.bus.send(protocol.NewSnapshot(e.State()))
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

func (e *Engine) bootstrap(ctx context.Context, p protocol.ConnectParams, gen uint64) error {
	e.eventf(protocol.LevelInfo, "authenticating against %s:%d", p.Host, p.Port)
	rpc, err := e.dial(ctx, p)
	if err != nil {
		return err
	}
	// rpc is authenticated from here on: if this bootstrap does not commit,
	// its consoles and login must not linger on the daemon
	var conID, routeID string
	committed := false
	defer func() {
		if !committed {
			release(rpc, conID, routeID)
		}
	}()

	version, err := gomsf.NewCoreManager(rpc).Version(ctx)
	if err != nil {
		return err
	}
	e.eventf(protocol.LevelInfo, "metasploit %s, loading module list", version.Version)

	mm := gomsf.NewModuleManager(rpc)
	modules := &protocol.ModuleIndex{}
	if modules.Exploits, err = mm.Exploits(ctx); err != nil {
		return err
	}
	if modules.Auxiliary, err = mm.Auxiliary(ctx); err != nil {
		return err
	}
	if modules.Post, err = mm.Post(ctx); err != nil {
		return err
	}
	if modules.Payloads, err = mm.Payloads(ctx); err != nil {
		return err
	}
	if modules.Encoders, err = mm.Encoders(ctx); err != nil {
		return err
	}
	if modules.Nops, err = mm.Nops(ctx); err != nil {
		return err
	}
	if modules.Evasion, err = mm.Evasion(ctx); err != nil {
		return err
	}

	db := gomsf.NewDbManager(rpc)
	hosts, err := db.Hosts(ctx, nil)
	if err != nil {
		return err
	}
	services, err := db.Services(ctx, nil)
	if err != nil {
		return err
	}
	creds, err := db.Creds(ctx, nil)
	if err != nil {
		return err
	}
	loots, err := db.Loots(ctx, nil)
	if err != nil {
		return err
	}
	workspace, _ := db.CurrentWorkspace(ctx) // "" when no db; not fatal

	e.eventf(protocol.LevelInfo, "loaded %d modules, reading database", len(modules.Exploits)+len(modules.Auxiliary)+len(modules.Post)+len(modules.Payloads)+len(modules.Encoders)+len(modules.Nops)+len(modules.Evasion))

	con, err := gomsf.NewConsoleManager(rpc).Create(ctx)
	if err != nil {
		return fmt.Errorf("create console: %w", err)
	}
	conID = con.ID
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

	runCtx, cancel := context.WithCancel(context.Background())
	monitor := gomsf.NewEventMonitor(runCtx, rpc,
		gomsf.WithEventSessionInterval(e.cfg.SessionInterval),
		gomsf.WithEventJobInterval(e.cfg.JobInterval),
		gomsf.WithEventOutputInterval(e.cfg.OutputInterval),
	)
	console := gomsf.NewMsfConsole(rpc, con.ID)

	// Commit: swap in the fresh rpc handle and state, then start the loops.
	// A newer generation (disconnect or another connect that finished first)
	// discards this attempt instead of clobbering the newer state.
	e.mu.Lock()
	if e.gen != gen {
		e.mu.Unlock()
		cancel() // our monitor's ctx; nobody else owns it
		return errSuperseded // the defer releases this attempt's consoles
	}
	oldCancel := e.runCancel
	oldRPC := e.rpc
	oldConsoleID := e.consoleID
	oldRouteCID := ""
	if e.routeConsole != nil {
		oldRouteCID = e.routeConsole.CID
	}
	e.rpc = rpc
	e.runCtx = runCtx
	e.runCancel = cancel
	e.monitor = monitor
	e.console = console
	e.consoleID = con.ID
	e.consoleOut = nil
	e.consolePrompt = ""
	e.routeConsole = routeCon
	e.routes = nil
	e.conn.MSFVersion = version.Version
	e.conn.Workspace = workspace
	e.conn.Error = ""
	e.modules = modules
	e.hosts = hostStates(hosts)
	e.services = serviceStates(services)
	e.creds = credStates(creds)
	e.loot = lootStates(loots)
	e.sessions = make(map[string]*protocol.SessionState)
	e.jobs = make(map[string]*protocol.JobState)
	e.errStreak = 0
	e.gen = gen + 1
	e.mu.Unlock()
	committed = true // the engine owns rpc and both consoles from the swap on
	if oldCancel != nil {
		oldCancel()
	}
	go release(oldRPC, oldConsoleID, oldRouteCID)

	if workspace == "" {
		e.eventf(protocol.LevelWarn, "msf database not connected; hosts and credentials views will stay empty")
	}

	go e.ingest(monitor, monitor.C())
	go e.consoleLoop(runCtx, monitor, console)
	if routeCon != nil {
		go e.routeLoop(runCtx, monitor, routeCon)
	}
	go e.refreshLoop(runCtx)
	go e.rankPrefetch(runCtx, rpc)
	return nil
}

// Disconnect drops the msfrpcd link. Discovered data (hosts, creds, events)
// is kept; live state (sessions, jobs, console) is rebuilt on next connect.
func (e *Engine) Disconnect() {
	e.mu.Lock()
	e.gen++ // invalidates any bootstrap or refresh still in flight
	cancel := e.runCancel
	dropRPC := e.rpc
	dropConsoleID := e.consoleID
	dropRouteCID := ""
	if e.routeConsole != nil {
		dropRouteCID = e.routeConsole.CID
	}
	e.runCancel = nil
	e.rpc = nil
	e.monitor = nil
	e.console = nil
	e.consoleID = ""
	e.consolePrompt = ""
	e.routeConsole = nil
	hadRoutes := len(e.routes) > 0
	e.routes = nil
	e.interactSID = ""
	e.interactOut = nil
	if e.conn.Status != "disconnected" {
		e.conn.Status = "disconnected"
		e.conn.Error = ""
		e.logf(protocol.LevelInfo, "disconnected from %s:%d", e.conn.Host, e.conn.Port)
	}
	e.password = ""
	e.lastPassword = ""
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	e.bus.send(protocol.ConnectionUpdate(e.snapshotConn()))
	e.bus.send(protocol.InteractUpdate(&protocol.InteractState{}))
	if hadRoutes {
		e.bus.send(protocol.RoutesUpdate([]*protocol.RouteState{}))
	}
	// consoles destroyed and the client logged out only after the operator
	// saw the disconnect; bounded so a dead daemon cannot wedge this call
	release(dropRPC, dropConsoleID, dropRouteCID)
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
