package engine

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// module.info is the only rank source and it answers one module at a time,
// so the whole catalogue is crawled in the background: paced workers after
// connect completes, results shipped in batches, cache kept across
// reconnects so a second connect only fills the gaps.

const (
	rankWorkers   = 4
	rankPace      = 20 * time.Millisecond
	rankFlushSize = 250
)

type rankTarget struct {
	name    string
	modType gomsf.ModuleType
}

func (e *Engine) rankTargets() []rankTarget {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modules == nil {
		return nil
	}
	targets := make([]rankTarget, 0, len(e.modules.Exploits)+len(e.modules.Auxiliary)+len(e.modules.Post))
	for _, n := range e.modules.Exploits {
		targets = append(targets, rankTarget{name: n, modType: gomsf.ExploitModuleType})
	}
	for _, n := range e.modules.Auxiliary {
		targets = append(targets, rankTarget{name: n, modType: gomsf.AuxiliaryModuleType})
	}
	for _, n := range e.modules.Post {
		targets = append(targets, rankTarget{name: n, modType: gomsf.PostModuleType})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	return targets
}

func (e *Engine) rankPrefetch(ctx context.Context, rpc gomsf.RPCCaller) {
	// snapshot the cached names: the live map keeps mutating under flush,
	// and reading it here without the lock would race
	cached := make(map[string]bool)
	e.mu.Lock()
	for name := range e.moduleRanks {
		cached[name] = true
	}
	e.mu.Unlock()

	var (
		mu    sync.Mutex
		batch = map[string]string{}
	)
	flush := func() {
		mu.Lock()
		if len(batch) == 0 {
			mu.Unlock()
			return
		}
		out := batch
		batch = map[string]string{}
		mu.Unlock()
		e.mu.Lock()
		if e.moduleRanks == nil {
			e.moduleRanks = make(map[string]string, len(cached)+len(out))
		}
		for k, v := range out {
			e.moduleRanks[k] = v
		}
		e.mu.Unlock()
		e.bus.send(protocol.ModuleRanksUpdate(out))
	}

	work := make(chan rankTarget)
	var wg sync.WaitGroup
	for i := 0; i < rankWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range work {
				if ctx.Err() != nil {
					return
				}
				info, err := gomsf.NewModuleManager(rpc).Info(ctx, t.modType, t.name)
				if err != nil {
					continue // uncached; a later connect retries
				}
				mu.Lock()
				batch[t.name] = info.Rank
				full := len(batch) >= rankFlushSize
				mu.Unlock()
				if full {
					flush()
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(rankPace):
				}
			}
		}()
	}

	for _, t := range e.rankTargets() {
		if ctx.Err() != nil {
			break
		}
		if cached[t.name] {
			continue
		}
		select {
		case <-ctx.Done():
			break
		case work <- t:
		}
	}
	close(work)
	wg.Wait()
	flush()
}
