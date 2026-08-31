package engine

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// msfrpcd has no route RPC, so the engine owns a second, operator-invisible
// console and polls `route` there. The table rows are the only route source
// that carries subnet and gateway; the add/remove confirmations do not.

var maskOctetBits = map[int]int{
	0: 0, 128: 1, 192: 2, 224: 3, 240: 4, 248: 5, 252: 6, 254: 7, 255: 8,
}

// maskToPrefix turns a dotted-quad netmask into a CIDR prefix length; ""
// for anything else (IPv6 masks keep their raw form).
func maskToPrefix(mask string) string {
	parts := strings.Split(mask, ".")
	if len(parts) != 4 {
		return ""
	}
	total := 0
	partial := false
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return ""
		}
		bits, ok := maskOctetBits[n]
		if !ok || (partial && n != 0) {
			return ""
		}
		total += bits
		if bits < 8 {
			partial = true
		}
	}
	return strconv.Itoa(total)
}

// parseRoutes reads the `route` console table. A row is "subnet netmask
// Session N"; the gateway's space makes it four whitespace fields. Only
// session gateways are pivots; local routes are not tracked.
func parseRoutes(data string) []*protocol.RouteState {
	var routes []*protocol.RouteState
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[2] != "Session" {
			continue
		}
		subnet := fields[0]
		if prefix := maskToPrefix(fields[1]); prefix != "" {
			subnet += "/" + prefix
		}
		routes = append(routes, &protocol.RouteState{Subnet: subnet, SessionID: fields[3]})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Subnet != routes[j].Subnet {
			return routes[i].Subnet < routes[j].Subnet
		}
		return routes[i].SessionID < routes[j].SessionID
	})
	return routes
}

func routeKey(r *protocol.RouteState) string {
	return r.Subnet + " via " + r.SessionID
}

func (e *Engine) routeLoop(ctx context.Context, monitor *gomsf.EventMonitor, console *gomsf.MsfConsole) {
	e.pollRoutes(ctx, monitor, console)
	ticker := time.NewTicker(e.cfg.RouteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.pollRoutes(ctx, monitor, console)
		case <-e.routeKick:
			e.pollRoutes(ctx, monitor, console)
		}
	}
}

// kickRoutePoll asks the route loop for an immediate poll. The channel is
// buffered and the send non-blocking: with no loop running the kick just
// sits for the next connection's first poll, which is harmless.
func (e *Engine) kickRoutePoll() {
	select {
	case e.routeKick <- struct{}{}:
	default:
	}
}

func (e *Engine) pollRoutes(ctx context.Context, monitor *gomsf.EventMonitor, console *gomsf.MsfConsole) {
	if err := console.Write(ctx, "route\n"); err != nil {
		e.monitorError(monitor, err)
		return
	}
	timer := time.NewTimer(e.cfg.OutputInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	// msfrpcd streams the table as it prints; a busy read carries a partial
	// table that would read as mass removals. Keep draining until the prompt
	// returns, bounded so a wedged console cannot stall the loop forever.
	// Each read drains only what printed since the last one, so the chunks
	// assemble into the full table.
	result, err := console.Read(ctx)
	if err != nil {
		e.monitorError(monitor, err)
		return
	}
	data := result.Data
	for i := 0; err == nil && result.Busy && i < 10; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.cfg.OutputInterval):
		}
		result, err = console.Read(ctx)
		if err == nil {
			data += result.Data
		}
	}
	if err != nil {
		e.monitorError(monitor, err)
		return
	}
	if result.Busy {
		return // table never settled; the next poll retries
	}
	routes := parseRoutes(cleanOutput(data))

	e.mu.Lock()
	if e.routeConsole != console {
		e.mu.Unlock()
		return
	}
	if sameRoutes(e.routes, routes) {
		e.mu.Unlock()
		return
	}
	current := make(map[string]bool, len(e.routes))
	for _, r := range e.routes {
		current[routeKey(r)] = true
	}
	next := make(map[string]bool, len(routes))
	for _, r := range routes {
		next[routeKey(r)] = true
	}
	for _, r := range e.routes {
		if !next[routeKey(r)] {
			e.logf(protocol.LevelInfo, "route removed: %s through session %s", r.Subnet, r.SessionID)
		}
	}
	for _, r := range routes {
		if !current[routeKey(r)] {
			e.logf(protocol.LevelInfo, "route added: %s through session %s", r.Subnet, r.SessionID)
		}
	}
	e.routes = routes
	e.mu.Unlock()
	e.bus.send(protocol.RoutesUpdate(routes))
}

func sameRoutes(a, b []*protocol.RouteState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if *a[i] != *b[i] {
			return false
		}
	}
	return true
}
