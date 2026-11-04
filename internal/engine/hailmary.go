package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Hail Mary is the Armitage signature move: fire every exploit the matcher
// offers at the chosen hosts and see what lands. Launches are paced so one
// click does not stampede msfrpcd, and every launch lands in the shared
// event log with operator attribution like any other.

const (
	hailMaryDefaultPerHost = 10
	hailMaryMaxPerHost     = 50
	hailMaryPace           = 500 * time.Millisecond
)

type hailMaryTarget struct {
	host    *protocol.HostState
	matches []protocol.AttackMatch
}

func (e *Engine) hailMary(ctx context.Context, operator, ws string, p protocol.HailMaryParams) (json.RawMessage, *protocol.ErrorBody) {
	if len(p.Hosts) == 0 {
		return nil, &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "no hosts given"}
	}
	if p.MaxPerHost <= 0 {
		p.MaxPerHost = hailMaryDefaultPerHost
	}
	if p.MaxPerHost > hailMaryMaxPerHost {
		p.MaxPerHost = hailMaryMaxPerHost
	}

	hostSet := make(map[string]bool, len(p.Hosts))
	for _, h := range p.Hosts {
		hostSet[h] = true
	}

	// Copy the immutable host/service pointers under the lock, then do all
	// catalogue matching outside it: preparing thousands of module names and
	// matching them per host must not stall broadcasts.
	type candidate struct {
		host *protocol.HostState
		svcs []*protocol.ServiceState
	}
	e.mu.Lock()
	var exploits []string
	if e.modules != nil {
		exploits = append([]string(nil), e.modules.Exploits...)
	}
	var candidates []candidate
	for _, h := range e.hosts {
		if h == nil || !hostSet[h.Address] {
			continue
		}
		var svcs []*protocol.ServiceState
		for _, s := range e.services {
			if s != nil && s.Host == h.Address {
				svcs = append(svcs, s)
			}
		}
		candidates = append(candidates, candidate{host: h, svcs: svcs})
	}
	e.mu.Unlock()

	catalogue := prepareAttacks(exploits)
	var targets []hailMaryTarget
	for _, c := range candidates {
		matches := matchPreparedAttacks(catalogue, c.svcs, c.host.OSName)
		if len(matches) > p.MaxPerHost {
			matches = matches[:p.MaxPerHost]
		}
		targets = append(targets, hailMaryTarget{host: c.host, matches: matches})
	}

	if len(targets) == 0 {
		return nil, &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "none of those hosts are in the workspace"}
	}

	planned := 0
	for _, t := range targets {
		planned += len(t.matches)
	}

	id, ctx, eb := e.beginOperation(protocol.OpHailMary, operator, ws,
		strings.Join(p.Hosts, ","), 0, planned)
	if eb != nil {
		return nil, eb
	}
	go e.runHailMary(ctx, id, operator, ws, targets, planned)

	return mustJSON(protocol.HailMaryPayload{Planned: planned, OperationID: id}), nil
}

func (e *Engine) runHailMary(ctx context.Context, opID, operator, ws string, targets []hailMaryTarget, planned int) {
	status, summary := protocol.OpFailed, "hail mary failed"
	defer func() { e.finishOperation(opID, status, summary) }()

	launched := 0
	for _, t := range targets {
		if len(t.matches) == 0 {
			e.eventfOpIn(ws, operator, protocol.LevelWarn, "hail mary: no matching exploits for %s", t.host.Address)
			continue
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "hail mary on %s: launching %d exploits", t.host.Address, len(t.matches))
		for _, m := range t.matches {
			if ctx.Err() != nil {
				status = protocol.OpCanceled
				summary = fmt.Sprintf("hail mary canceled after %d of %d launches", launched, planned)
				e.eventfOpIn(ws, operator, protocol.LevelWarn, "hail mary aborted after %d launches", launched)
				return
			}
			options := map[string]interface{}{"RHOSTS": t.host.Address}
			// attack the port the matching service was found on; the
			// module default is a guess, the scan is not
			if m.Port > 0 {
				options["RPORT"] = m.Port
			}
			if _, eb := e.moduleExecute(ctx, e.connectedRPC(), operator, ws, protocol.ModuleExecuteParams{
				Type: "exploit", Name: m.Name,
				Options: options,
			}); eb == nil {
				launched++
			}
			e.advanceOperation(opID, launched, fmt.Sprintf("launched %d of %d", launched, planned))
			select {
			case <-ctx.Done():
			case <-time.After(hailMaryPace):
			}
		}
	}
	if ctx.Err() != nil {
		status = protocol.OpCanceled
		summary = fmt.Sprintf("hail mary canceled after %d of %d launches", launched, planned)
		e.eventfOpIn(ws, operator, protocol.LevelWarn, "%s", summary)
		return
	}
	status = protocol.OpDone
	summary = fmt.Sprintf("%d of %d planned launches", launched, planned)
	e.eventfOpIn(ws, operator, protocol.LevelSuccess, "hail mary finished: %d of %d planned launches", launched, planned)
}
