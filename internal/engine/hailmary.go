package engine

import (
	"context"
	"encoding/json"
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

func (e *Engine) hailMary(ctx context.Context, operator string, p protocol.HailMaryParams) (json.RawMessage, *protocol.ErrorBody) {
	if len(p.Hosts) == 0 {
		return nil, &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "no hosts given"}
	}
	if p.MaxPerHost <= 0 {
		p.MaxPerHost = hailMaryDefaultPerHost
	}
	if p.MaxPerHost > hailMaryMaxPerHost {
		p.MaxPerHost = hailMaryMaxPerHost
	}

	e.mu.Lock()
	var exploits []string
	if e.modules != nil {
		exploits = e.modules.Exploits
	}
	hostSet := make(map[string]bool, len(p.Hosts))
	for _, h := range p.Hosts {
		hostSet[h] = true
	}
	var targets []hailMaryTarget
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
		matches := matchAttacks(exploits, svcs, h.OSName)
		if len(matches) > p.MaxPerHost {
			matches = matches[:p.MaxPerHost]
		}
		targets = append(targets, hailMaryTarget{host: h, matches: matches})
	}
	e.mu.Unlock()

	if len(targets) == 0 {
		return nil, &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "none of those hosts are in the workspace"}
	}

	planned := 0
	for _, t := range targets {
		planned += len(t.matches)
	}

	runCtx := e.runContext()
	go func() {
		launched := 0
		for _, t := range targets {
			if len(t.matches) == 0 {
				e.eventfOp(operator, protocol.LevelWarn, "hail mary: no matching exploits for %s", t.host.Address)
				continue
			}
			e.eventfOp(operator, protocol.LevelInfo, "hail mary on %s: launching %d exploits", t.host.Address, len(t.matches))
			for _, m := range t.matches {
				if runCtx.Err() != nil {
					e.eventfOp(operator, protocol.LevelWarn, "hail mary aborted after %d launches: connection ended", launched)
					return
				}
				if _, eb := e.moduleExecute(runCtx, operator, protocol.ModuleExecuteParams{
					Type: "exploit", Name: m.Name,
					Options: map[string]interface{}{"RHOSTS": t.host.Address},
				}); eb != nil {
					continue
				}
				launched++
				select {
				case <-runCtx.Done():
				case <-time.After(hailMaryPace):
				}
			}
		}
		e.eventfOp(operator, protocol.LevelSuccess, "hail mary finished: %d of %d planned launches", launched, planned)
	}()

	return mustJSON(protocol.HailMaryPayload{Planned: planned}), nil
}
