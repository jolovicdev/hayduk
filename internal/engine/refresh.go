package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// refreshLoop polls the msf database until its run context ends. The ctx is
// handed in by the bootstrap that owns it, so an old loop can never bind to
// a newer connection's context.
func (e *Engine) refreshLoop(ctx context.Context) {
	e.refreshDB(ctx)
	e.reconcileSessions(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.cfg.RefreshInterval):
			e.refreshDB(ctx)
			e.reconcileSessions(ctx)
		}
	}
}

func (e *Engine) runContext() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runCtx
}

// refreshDB pulls hosts/services/creds/loot/workspace and diffs against
// current state, emitting events for what is new. Errors are logged as
// events and do not stop the loop.
var errNotConnected = errors.New("not connected to msfrpcd")

func (e *Engine) refreshDB(ctx context.Context) error {
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	return e.refreshDBLocked(ctx)
}

const (
	// dbPageSize bounds one db.* RPC read. msf caps unpaged reads at its own
	// default limit of 100 rows, so one naked call silently truncates every
	// collection of a larger workspace.
	dbPageSize = 200
	// dbMaxPages bounds the page walk so a daemon answering every page full
	// (or ignoring offset entirely) cannot loop forever: 200 x 500 = 100k
	// rows per collection.
	dbMaxPages = 500
)

// fetchAllPages walks a db collection's limit/offset pages until a short page
// arrives, pinning the workspace into every request. All pages of one
// collection are fetched or none are committed: callers get an error, never a
// silently partial collection.
func fetchAllPages[T any](ctx context.Context, workspace string, fetch func(ctx context.Context, opts map[string]interface{}) ([]*T, error)) ([]*T, error) {
	var out []*T
	for page := 0; page < dbMaxPages; page++ {
		rows, err := fetch(ctx, map[string]interface{}{
			"workspace": workspace,
			"limit":     dbPageSize,
			"offset":    page * dbPageSize,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < dbPageSize {
			return out, nil
		}
	}
	return nil, fmt.Errorf("db collection exceeds %d rows", dbMaxPages*dbPageSize)
}

// refreshDBLocked is refreshDB for callers already holding refreshMu -
// workspace.set uses it so its clear-plus-reload transition cannot be
// interleaved with a periodic refresh that still read the old workspace.
func (e *Engine) refreshDBLocked(ctx context.Context) error {
	e.mu.Lock()
	rpc := e.rpc
	gen := e.gen
	pinned := e.conn.Workspace
	e.mu.Unlock()
	if rpc == nil {
		return errNotConnected
	}

	db := gomsf.NewDbManager(rpc)
	// The workspace is read once and pinned into every collection page: an
	// operator switching workspaces in the raw msf console mid-refresh must
	// not mix one refresh's rows across two workspaces.
	if current, _ := db.CurrentWorkspace(ctx); current != "" {
		pinned = current
	}
	hosts, err := fetchAllPages(ctx, pinned, db.Hosts)
	if err != nil {
		e.eventfOpIn(pinned, "", protocol.LevelWarn, "db refresh failed: %v", err)
		return err
	}
	services, err := fetchAllPages(ctx, pinned, db.Services)
	if err != nil {
		e.eventfOpIn(pinned, "", protocol.LevelWarn, "db refresh failed: %v", err)
		return err
	}
	creds, err := fetchAllPages(ctx, pinned, db.Creds)
	if err != nil {
		e.eventfOpIn(pinned, "", protocol.LevelWarn, "db refresh failed: %v", err)
		return err
	}
	loots, err := fetchAllPages(ctx, pinned, db.Loots)
	if err != nil {
		e.eventfOpIn(pinned, "", protocol.LevelWarn, "db refresh failed: %v", err)
		return err
	}
	workspace := pinned

	newHosts := hostStates(hosts)
	newServices := serviceStates(services)
	newCreds := credStates(creds)
	newLoot := lootStates(loots)

	e.mu.Lock()
	if e.gen != gen || e.rpc == nil {
		// the link was torn down or replaced while this refresh was in
		// flight; committing would clobber the newer connection's state
		e.mu.Unlock()
		return errSuperseded
	}
	known := make(map[string]bool, len(e.hosts))
	for _, h := range e.hosts {
		known[h.Address] = true
	}
	var discovered []*protocol.HostState
	for _, h := range newHosts {
		if !known[h.Address] {
			discovered = append(discovered, h)
		}
	}
	credDelta := len(newCreds) - len(e.creds)

	hostsChanged := !sameHosts(e.hosts, newHosts)
	servicesChanged := !sameServices(e.services, newServices)
	credsChanged := !sameCreds(e.creds, newCreds)
	lootChanged := !sameLoot(e.loot, newLoot)
	wsChanged := workspace != "" && workspace != e.conn.Workspace

	e.hosts = newHosts
	e.services = newServices
	e.creds = newCreds
	e.loot = newLoot
	// the round trip proves the link healthy; the monitor's streak starts over
	e.errStreak = 0
	if wsChanged {
		e.conn.Workspace = workspace
		e.logf(protocol.LevelInfo, "workspace changed to %s", workspace)
	}
	// discovered rows belong to the workspace this refresh pinned, not to
	// whatever the connection says by the time the commit lands
	for _, h := range discovered {
		e.logfIn(workspace, "", protocol.LevelInfo, "discovered host %s (%s)", h.Address, hostLabel(h.Name))
	}
	if credDelta > 0 {
		e.logfIn(workspace, "", protocol.LevelSuccess, "%d new credentials", credDelta)
	}
	if wsChanged {
		e.bus.send(protocol.ConnectionUpdate(e.conn))
	}
	if hostsChanged {
		e.bus.send(protocol.HostsUpdate(newHosts))
	}
	if servicesChanged {
		e.bus.send(protocol.ServicesUpdate(newServices))
	}
	if credsChanged {
		e.bus.send(protocol.CredsUpdate(newCreds))
	}
	if lootChanged {
		e.bus.send(protocol.LootUpdate(newLoot))
	}
	e.mu.Unlock()
	return nil
}

// The same-* comparators ignore order and compare content: a host whose os
// fingerprint ripened, or a service whose name resolved, must reach browsers
// even though the collection length did not move.

func sameHosts(a, b []*protocol.HostState) bool {
	if len(a) != len(b) {
		return false
	}
	sig := func(h *protocol.HostState) string {
		return strings.Join([]string{
			h.Address, h.Name, h.Mac, h.OSName, h.OSFlavor, h.OSVersion, h.Purpose, h.Info, h.Comments,
		}, "\x1f")
	}
	return sameSigs(
		mapSigs(a, func(h *protocol.HostState) string { return h.Address + "\x1f" + sig(h) }),
		mapSigs(b, func(h *protocol.HostState) string { return h.Address + "\x1f" + sig(h) }),
	)
}

func sameServices(a, b []*protocol.ServiceState) bool {
	if len(a) != len(b) {
		return false
	}
	sig := func(s *protocol.ServiceState) string {
		return s.Host + "\x1f" + s.Proto + "\x1f" + strconv.Itoa(s.Port) + "\x1f" +
			s.Name + "\x1f" + s.State + "\x1f" + s.Info
	}
	return sameSigs(mapSigs(a, sig), mapSigs(b, sig))
}

func sameCreds(a, b []*protocol.CredState) bool {
	if len(a) != len(b) {
		return false
	}
	sig := func(c *protocol.CredState) string {
		return c.Host + "\x1f" + c.Proto + "\x1f" + strconv.Itoa(c.Port) + "\x1f" +
			c.Service + "\x1f" + c.User + "\x1f" + c.Pass + "\x1f" + c.Type
	}
	return sameSigs(mapSigs(a, sig), mapSigs(b, sig))
}

func sameLoot(a, b []*protocol.LootState) bool {
	if len(a) != len(b) {
		return false
	}
	sig := func(l *protocol.LootState) string {
		return l.Host + "\x1f" + l.Type + "\x1f" + l.Name + "\x1f" + l.Info
	}
	return sameSigs(mapSigs(a, sig), mapSigs(b, sig))
}

func mapSigs[T any](in []*T, sig func(*T) string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, sig(v))
	}
	sort.Strings(out)
	return out
}

func sameSigs(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
