package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// execCommand runs one dispatched command; the engine lock is NOT held.
func (e *Engine) execCommand(ctx context.Context, operator, method string, params json.RawMessage) (json.RawMessage, *protocol.ErrorBody) {
	if !knownMethod(method) {
		return nil, &protocol.ErrorBody{Code: protocol.CodeUnknownMethod, Message: "no such method: " + method}
	}
	if eb := requireConnected(e); eb != nil {
		return nil, eb
	}

	switch method {
	case protocol.MethodConsoleWrite:
		var p protocol.ConsoleWriteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Command == "" {
			return nil, badParam("command is required")
		}
		con := e.currentConsole()
		if con == nil {
			return nil, notConnected()
		}
		if err := con.Write(ctx, p.Command); err != nil {
			return nil, mapErr(err)
		}
		return nil, nil

	case protocol.MethodConsoleTabs:
		var p protocol.ConsoleTabsParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		con := e.currentConsole()
		if con == nil {
			return nil, notConnected()
		}
		tabs, err := con.Tabs(ctx, p.Line)
		if err != nil {
			return nil, mapErr(err)
		}
		return mustJSON(tabs), nil

	case protocol.MethodModuleInfo:
		var p protocol.ModuleRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		info, err := gomsf.NewModuleManager(e.rpcClient()).Info(ctx, gomsf.ModuleType(p.Type), p.Name)
		if err != nil {
			return nil, mapErr(err)
		}
		return mustJSON(moduleInfoPayload(info)), nil

	case protocol.MethodModuleOptions:
		var p protocol.ModuleRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		mod, err := gomsf.NewModuleWithContext(ctx, e.rpcClient(), gomsf.ModuleType(p.Type), p.Name)
		if err != nil {
			return nil, mapErr(err)
		}
		opts := make(map[string]*protocol.ModuleOptionPayload)
		for _, name := range mod.Options() {
			oi, _ := mod.OptionInfo(name)
			opts[name] = &protocol.ModuleOptionPayload{
				Type: oi.Type, Required: oi.Required, Advanced: oi.Advanced,
				Desc: oi.Desc, Default: oi.Default, Enums: oi.Enums,
			}
		}
		return mustJSON(opts), nil

	case protocol.MethodPayloads:
		var p protocol.ModuleRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		payloads, err := gomsf.NewModuleManager(e.rpcClient()).CompatiblePayloads(ctx, p.Name)
		if err != nil {
			return nil, mapErr(err)
		}
		return mustJSON(payloads), nil

	case protocol.MethodModuleExecute:
		var p protocol.ModuleExecuteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		return e.moduleExecute(ctx, operator, p)

	case protocol.MethodSessionAttach:
		var p protocol.SessionRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		if eb := e.attach(p.SID); eb != nil {
			return nil, eb
		}
		return nil, nil

	case protocol.MethodSessionDetach:
		e.detach()
		return nil, nil

	case protocol.MethodSessionWrite:
		var p protocol.SessionWriteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		if p.Data == "" {
			return nil, badParam("data is required")
		}
		if eb := e.sessionWrite(ctx, p); eb != nil {
			return nil, eb
		}
		return nil, nil

	case protocol.MethodSessionStop:
		var p protocol.SessionRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		if err := gomsf.NewSessionManager(e.rpcClient()).Stop(ctx, p.SID); err != nil {
			return nil, mapErr(err)
		}
		return nil, nil

	case protocol.MethodSessionUpgrade:
		var p protocol.SessionUpgradeParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		if p.LHOST == "" {
			return nil, badParam("lhost is required")
		}
		if !validPort(p.LPORT) {
			return nil, badParam("lport must be between 1 and 65535")
		}
		if eb := e.sessionUpgrade(ctx, operator, p); eb != nil {
			return nil, eb
		}
		return nil, nil

	case protocol.MethodWorkspaceList:
		names, err := gomsf.NewDbManager(e.rpcClient()).Workspaces().List(ctx)
		if err != nil {
			return nil, mapErr(err)
		}
		return mustJSON(names), nil

	case protocol.MethodWorkspaceSet:
		var p protocol.WorkspaceSetParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Name == "" {
			return nil, badParam("name is required")
		}
		if err := gomsf.NewDbManager(e.rpcClient()).SetWorkspace(ctx, p.Name); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOp(operator, protocol.LevelInfo, "switched to workspace %s", p.Name)
		e.refreshDB(ctx)
		return nil, nil

	case protocol.MethodDBRefresh:
		if err := e.refreshDB(ctx); err != nil {
			return nil, mapErr(err)
		}
		return nil, nil

	case protocol.MethodAttacksFind:
		var p protocol.AttacksFindParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Host == "" {
			return nil, badParam("host is required")
		}
		return e.attacksFind(p.Host)

	case protocol.MethodHailMary:
		var p protocol.HailMaryParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		return e.hailMary(ctx, operator, p)
	}
	return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "unreachable dispatch for " + method}
}

func (e *Engine) moduleExecute(ctx context.Context, operator string, p protocol.ModuleExecuteParams) (json.RawMessage, *protocol.ErrorBody) {
	// JSON numbers decode as float64; msf's option validator rejects floats
	// for integer options, so integral values become integers first
	normalizeNumericOptions(p.Options)
	normalizeNumericOptions(p.PayloadOptions)

	rpc := e.rpcClient()
	mod, err := gomsf.NewModuleWithContext(ctx, rpc, gomsf.ModuleType(p.Type), p.Name)
	if err != nil {
		return nil, mapErr(err)
	}
	for k, v := range p.Options {
		if err := mod.SetOption(k, v); err != nil {
			return nil, mapErr(err)
		}
	}

	var res *gomsf.ModuleExecuteResult
	if p.Payload != "" {
		payload, perr := gomsf.NewModuleWithContext(ctx, rpc, gomsf.PayloadModuleType, p.Payload)
		if perr != nil {
			return nil, mapErr(perr)
		}
		for k, v := range p.PayloadOptions {
			if err := payload.SetOption(k, v); err != nil {
				return nil, mapErr(err)
			}
		}
		res, err = executeWithPayload(ctx, rpc, mod, payload)
	} else {
		res, err = mod.Execute(ctx)
	}
	if err != nil {
		e.eventfOp(operator, protocol.LevelError, "%s/%s failed: %v", p.Type, p.Name, err)
		return nil, mapErr(err)
	}
	// msf answers job 0 when the module runs inline and finishes at once -
	// there is no job to watch then, and saying "job 0" just confuses
	if res.JobID > 0 {
		e.eventfOp(operator, protocol.LevelSuccess, "%s/%s launched as job %d", p.Type, p.Name, res.JobID)
	} else {
		e.eventfOp(operator, protocol.LevelSuccess, "%s/%s ran inline", p.Type, p.Name)
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

// executeWithPayload mirrors Module.ExecuteWithPayload but drops non-scalar
// option values before sending: msf's rpc_execute rejects anything that is
// not a scalar, and payload defaults carry at least one array
// (AutoLoadExtensions), which would fail every payload launch.
func executeWithPayload(ctx context.Context, rpc gomsf.RPCCaller, mod, payload *gomsf.Module) (*gomsf.ModuleExecuteResult, error) {
	options := mod.RunOptions()
	options["PAYLOAD"] = payload.Name
	for k, v := range payload.RunOptions() {
		if _, ok := options[k]; !ok {
			options[k] = v
		}
	}
	for k, v := range options {
		switch v.(type) {
		case nil, string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
		default:
			delete(options, k)
		}
	}
	return gomsf.NewModuleManager(rpc).Execute(ctx, mod.ModuleType, mod.Name, options)
}

func (e *Engine) attach(sid string) *protocol.ErrorBody {
	e.mu.Lock()
	if e.sessions[sid] == nil {
		e.mu.Unlock()
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + sid}
	}
	previous := e.interactSID
	e.interactSID = sid
	e.interactOut = nil
	mon := e.monitor
	interact := &protocol.InteractState{SID: sid}
	e.mu.Unlock()
	if mon != nil {
		if previous != "" && previous != sid {
			mon.UnwatchSession(previous) // switching must not leave both polled
		}
		mon.WatchSession(sid)
	}
	e.bus.send(protocol.InteractUpdate(interact))
	return nil
}

func (e *Engine) detach() {
	e.mu.Lock()
	sid := e.interactSID
	if sid == "" {
		e.mu.Unlock()
		return
	}
	e.interactSID = ""
	e.interactOut = nil
	mon := e.monitor
	e.mu.Unlock()
	if mon != nil {
		mon.UnwatchSession(sid)
	}
	e.bus.send(protocol.InteractUpdate(&protocol.InteractState{}))
}

func (e *Engine) sessionWrite(ctx context.Context, p protocol.SessionWriteParams) *protocol.ErrorBody {
	e.mu.Lock()
	rpc := e.rpc
	attached := e.interactSID == p.SID
	session := e.sessions[p.SID]
	e.mu.Unlock()
	if rpc == nil {
		return notConnected()
	}
	if session == nil {
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + p.SID}
	}
	if !attached {
		return &protocol.ErrorBody{Code: protocol.CodeBusy, Message: "session " + p.SID + " is not attached; attach first"}
	}
	data := strings.TrimSuffix(p.Data, "\n") + "\n"
	if session.Type == "meterpreter" {
		return mapErr(gomsf.NewMeterpreterSession(rpc, p.SID).Write(ctx, data))
	}
	return mapErr(gomsf.NewShellSession(rpc, p.SID).Write(ctx, data))
}

func (e *Engine) sessionUpgrade(ctx context.Context, operator string, p protocol.SessionUpgradeParams) *protocol.ErrorBody {
	e.mu.Lock()
	rpc := e.rpc
	session := e.sessions[p.SID]
	e.mu.Unlock()
	if rpc == nil {
		return notConnected()
	}
	if session == nil {
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + p.SID}
	}
	if session.Type != "shell" {
		return &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "session " + p.SID + " is " + session.Type + ", only shell sessions upgrade"}
	}
	if err := gomsf.NewShellSession(rpc, p.SID).Upgrade(ctx, p.LHOST, p.LPORT); err != nil {
		return mapErr(err)
	}
	e.eventfOp(operator, protocol.LevelSuccess, "session %s upgrading to meterpreter via %s:%d", p.SID, p.LHOST, p.LPORT)
	return nil
}

func (e *Engine) currentConsole() *gomsf.MsfConsole {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.console
}

func (e *Engine) rpcClient() gomsf.RPCCaller {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rpc
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

func notConnected() *protocol.ErrorBody {
	return &protocol.ErrorBody{Code: protocol.CodeNotConnected, Message: "not connected to msfrpcd"}
}

func mapErr(err error) *protocol.ErrorBody {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, gomsf.ErrRPC):
		return &protocol.ErrorBody{Code: protocol.CodeRPC, Message: err.Error()}
	case errors.Is(err, gomsf.ErrUnexpectedResponse):
		return &protocol.ErrorBody{Code: protocol.CodeUnexpected, Message: err.Error()}
	case errors.Is(err, gomsf.ErrCommandTimeout):
		return &protocol.ErrorBody{Code: protocol.CodeTimeout, Message: err.Error()}
	case errors.Is(err, gomsf.ErrSessionNotFound):
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: err.Error()}
	case errors.Is(err, gomsf.ErrInvalidOption):
		return &protocol.ErrorBody{Code: protocol.CodeInvalidOption, Message: err.Error()}
	case errors.Is(err, gomsf.ErrNotAuthenticated):
		return &protocol.ErrorBody{Code: protocol.CodeNotConnected, Message: err.Error()}
	case errors.Is(err, errNotConnected):
		return &protocol.ErrorBody{Code: protocol.CodeNotConnected, Message: err.Error()}
	default:
		return &protocol.ErrorBody{Code: protocol.CodeInternal, Message: err.Error()}
	}
}

func knownMethod(method string) bool {
	switch method {
	case protocol.MethodConsoleWrite, protocol.MethodConsoleTabs,
		protocol.MethodModuleInfo, protocol.MethodModuleOptions, protocol.MethodPayloads,
		protocol.MethodModuleExecute, protocol.MethodSessionAttach, protocol.MethodSessionDetach,
		protocol.MethodSessionWrite, protocol.MethodSessionStop, protocol.MethodSessionUpgrade,
		protocol.MethodWorkspaceList, protocol.MethodWorkspaceSet, protocol.MethodDBRefresh,
		protocol.MethodAttacksFind, protocol.MethodHailMary:
		return true
	}
	return false
}
