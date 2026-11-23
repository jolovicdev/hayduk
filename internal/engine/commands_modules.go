package engine

import (
	"context"
	"encoding/json"

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
