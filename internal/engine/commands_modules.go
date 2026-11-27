package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func (e *Engine) moduleExecute(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.ModuleExecuteParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	// JSON numbers decode as float64; msf's option validator rejects floats
	// for integer options, so integral values become integers first
	normalizeNumericOptions(p.Options)
	normalizeNumericOptions(p.PayloadOptions)

	// Exploit and payload metadata are independent; load them at once. The
	// payload result is consumed below, and cancelLoads plus the buffered
	// result channel keep an abandoned payload load from outliving the
	// command.
	loadCtx, cancelLoads := context.WithCancel(ctx)
	defer cancelLoads()

	type moduleLoadResult struct {
		mod *gomsf.Module
		err error
	}
	loadModule := func(modType gomsf.ModuleType, name string) <-chan moduleLoadResult {
		ch := make(chan moduleLoadResult, 1)
		go func() {
			mod, err := gomsf.NewModuleWithContext(loadCtx, rpc, modType, name)
			ch <- moduleLoadResult{mod: mod, err: err}
		}()
		return ch
	}

	var payloadCh <-chan moduleLoadResult
	if p.Payload != "" {
		payloadCh = loadModule(gomsf.PayloadModuleType, p.Payload)
	}

	mod, err := gomsf.NewModuleWithContext(loadCtx, rpc, gomsf.ModuleType(p.Type), p.Name)
	if err != nil {
		cancelLoads()
		if payloadCh != nil {
			<-payloadCh
		}
		return nil, mapErr(err)
	}
	for k, v := range p.Options {
		if err := mod.SetOption(k, v); err != nil {
			cancelLoads()
			if payloadCh != nil {
				<-payloadCh
			}
			return nil, mapErr(err)
		}
	}

	// Register before the RPC so sessions and job events that arrive
	// before its response can be attributed to this launch.
	var runKey string
	if p.Type == string(gomsf.ExploitModuleType) && p.Name != handlerModule {
		runKey = e.beginExploitRun(p.Type+"/"+p.Name, ws, operator)
	}
	var res *gomsf.ModuleExecuteResult
	if p.Payload != "" {
		payloadRes := <-payloadCh
		if payloadRes.err != nil {
			if runKey != "" {
				e.forgetExploitRun(runKey)
			}
			return nil, mapErr(payloadRes.err)
		}
		payload := payloadRes.mod
		for k, v := range p.PayloadOptions {
			if err := payload.SetOption(k, v); err != nil {
				if runKey != "" {
					e.forgetExploitRun(runKey)
				}
				return nil, mapErr(err)
			}
		}
		res, err = executeWithPayload(ctx, rpc, mod, payload)
	} else {
		res, err = mod.Execute(ctx)
	}
	if err != nil {
		if runKey != "" {
			e.forgetExploitRun(runKey)
		}
		e.eventfOpIn(ws, operator, protocol.LevelError, "%s/%s failed: %v", p.Type, p.Name, err)
		return nil, mapErr(err)
	}
	// msf answers job 0 when the module runs inline and finishes at once -
	// there is no job to watch then, and saying "job 0" just confuses
	if res.JobID > 0 {
		e.eventfOpIn(ws, operator, protocol.LevelSuccess, "%s/%s launched as job %d", p.Type, p.Name, res.JobID)
	} else {
		e.eventfOpIn(ws, operator, protocol.LevelSuccess, "%s/%s ran inline", p.Type, p.Name)
	}
	if runKey != "" {
		e.armExploitRun(runKey, res.UUID, res.JobID)
	}
	// A handler launch is a listener: remember what it is listening for and
	// with which callback so the Listeners view survives a reload.
	if p.Type == string(gomsf.ExploitModuleType) && p.Name == handlerModule {
		e.registerListener(operator, res.JobID, p.Payload, p.PayloadOptions)
	}
	// Autoroute exists only to change the route table. A job-backed run
	// kicks the poller again when its job stops, but an inline run has no
	// job events at all, so kick here or the new pivot waits out the
	// periodic poll before it reaches the topology.
	if p.Type == "post" && p.Name == autorouteModule {
		e.kickRoutePoll()
	}
	return mustJSON(protocol.ExecPayload{JobID: res.JobID, UUID: res.UUID}), nil
}

func normalizeNumericOptions(options map[string]interface{}) {
	for k, v := range options {
		if f, ok := v.(float64); ok && f == float64(int64(f)) {
			options[k] = int64(f)
		}
	}
}

// executeWithPayload launches the module with the payload's defaults merged
// under the operator's options. go-msf v2.2.0 drops non-scalar option values
// before module.execute itself (payload defaults carry at least one array,
// AutoLoadExtensions, which msf's rpc_execute rejects).
func executeWithPayload(ctx context.Context, rpc gomsf.RPCCaller, mod, payload *gomsf.Module) (*gomsf.ModuleExecuteResult, error) {
	return mod.ExecuteWithPayload(ctx, payload)
}

const moduleCacheCap = 512

type moduleMetaFlight struct {
	done    chan struct{}
	infoRaw json.RawMessage
	optsRaw json.RawMessage
	infoErr error
	optsErr error
}

func (e *Engine) cachedModuleData(cache *map[string]json.RawMessage, key string) json.RawMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	if *cache == nil {
		return nil
	}
	return (*cache)[key]
}

func (e *Engine) storeModuleData(cache *map[string]json.RawMessage, rpc gomsf.RPCCaller, key string, raw json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rpc != rpc {
		return
	}
	if *cache == nil {
		*cache = make(map[string]json.RawMessage, 32)
	}
	if len(*cache) >= moduleCacheCap {
		clear(*cache)
	}
	(*cache)[key] = raw
}

// loadModuleMeta coalesces concurrent module.info and module.options calls for
// one module into a single NewModuleWithContext fetch. Real msfrpcd embeds the
// full options map inside module.info, so LaunchDialog pays one RPC instead of
// two on cold open and zero on reopen. If options fail on a fake/minimal daemon
// that only serves info, info still succeeds.
func (e *Engine) loadModuleMeta(ctx context.Context, rpc gomsf.RPCCaller, modType, name string, needOptions bool) (json.RawMessage, *protocol.ErrorBody) {
	key := modType + "/" + name
	if needOptions {
		if cached := e.cachedModuleData(&e.modOptionsCache, key); cached != nil {
			return cached, nil
		}
	} else {
		if cached := e.cachedModuleData(&e.modInfoCache, key); cached != nil {
			return cached, nil
		}
	}

	e.mu.Lock()
	if e.modMetaInflight == nil {
		e.modMetaInflight = make(map[string]*moduleMetaFlight)
	}
	flight, joined := e.modMetaInflight[key]
	if !joined {
		flight = &moduleMetaFlight{done: make(chan struct{})}
		e.modMetaInflight[key] = flight
	}
	e.mu.Unlock()

	if joined {
		select {
		case <-flight.done:
		case <-ctx.Done():
			return nil, mapErr(ctx.Err())
		}
	} else {
		// Run the shared fetch under a detached-from-single-caller context if
		// the engine is still alive so one cancelled caller doesn't poison a
		// joined waiter, while still respecting the caller context when no
		// runCtx exists (unit tests).
		fetchCtx := ctx
		if rc := e.runContext(); rc != nil {
			var cancel context.CancelFunc
			fetchCtx, cancel = context.WithTimeout(rc, 30*time.Second)
			defer cancel()
		}
		mod, err := gomsf.NewModuleWithContext(fetchCtx, rpc, gomsf.ModuleType(modType), name)
		if err == nil {
			if mod.Info != nil {
				flight.infoRaw = mustJSON(moduleInfoPayload(mod.Info))
				e.storeModuleData(&e.modInfoCache, rpc, key, flight.infoRaw)
			}
			opts := make(map[string]*protocol.ModuleOptionPayload)
			for _, optName := range mod.Options() {
				oi, _ := mod.OptionInfo(optName)
				opts[optName] = &protocol.ModuleOptionPayload{
					Type: oi.Type, Required: oi.Required, Advanced: oi.Advanced,
					Desc: oi.Desc, Default: oi.Default, Enums: oi.Enums,
				}
			}
			flight.optsRaw = mustJSON(opts)
			e.storeModuleData(&e.modOptionsCache, rpc, key, flight.optsRaw)
		} else {
			flight.optsErr = err
			// Fall back to bare module.info so callers that only need info still
			// work when module.options is unavailable.
			info, infoErr := gomsf.NewModuleManager(rpc).Info(fetchCtx, gomsf.ModuleType(modType), name)
			if infoErr == nil {
				flight.infoRaw = mustJSON(moduleInfoPayload(info))
				e.storeModuleData(&e.modInfoCache, rpc, key, flight.infoRaw)
			} else {
				flight.infoErr = infoErr
			}
		}
		e.mu.Lock()
		if e.modMetaInflight[key] == flight {
			delete(e.modMetaInflight, key)
		}
		e.mu.Unlock()
		close(flight.done)
	}

	if needOptions {
		if flight.optsErr != nil {
			return nil, mapErr(flight.optsErr)
		}
		return flight.optsRaw, nil
	}
	if flight.infoErr != nil {
		return nil, mapErr(flight.infoErr)
	}
	if flight.infoRaw == nil {
		return nil, mapErr(flight.optsErr)
	}
	return flight.infoRaw, nil
}

func moduleInfoPayload(info *gomsf.MsfModuleInfo) protocol.ModuleInfoPayload {
	out := protocol.ModuleInfoPayload{
		Name: info.Name, Description: info.Description, Rank: info.Rank,
		Authors: info.Authors, Targets: info.Targets, FilePath: info.FilePath,
	}
	for _, r := range info.References {
		out.References = append(out.References, protocol.ModuleRefPayload{Type: r.Type, Value: r.Value})
	}
	return out
}
