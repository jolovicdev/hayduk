package engine

import (
	"bytes"
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
	// the workspace the command starts in rides with its events: switching
	// workspaces mid-RPC must not re-attribute the result
	rpc, ws := e.dispatchScope()
	if rpc == nil {
		return nil, notConnected()
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
		// Readiness is restored only when a read issued after this write
		// reports the console idle (consoleRead checks the generation):
		// the buffered prompt must not go out here, or browsers accept
		// input while msf still runs the command.
		e.mu.Lock()
		if e.console != nil {
			// msfrpcd's web consoles do not echo commands. Store the echo
			// in the engine buffer so every browser receives it on replay.
			// Reuse the trailing idle prompt to avoid displaying it twice.
			echo := e.consolePrompt + strings.TrimRight(p.Command, "\n") + "\n"
			if e.consolePrompt != "" && bytes.HasSuffix(e.consoleOut, []byte(e.consolePrompt)) {
				e.consoleOut = e.consoleOut[:len(e.consoleOut)-len(e.consolePrompt)]
			}
			e.consoleOut = appendCapped(e.consoleOut, []byte(echo))
			e.bus.send(protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Data: echo})
			e.consoleWritePending = true
			e.consoleWriteGen++
		}
		e.mu.Unlock()
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
		info, err := gomsf.NewModuleManager(rpc).Info(ctx, gomsf.ModuleType(p.Type), p.Name)
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
		mod, err := gomsf.NewModuleWithContext(ctx, rpc, gomsf.ModuleType(p.Type), p.Name)
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
		payloads, err := gomsf.NewModuleManager(rpc).CompatiblePayloads(ctx, p.Name)
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
		return e.moduleExecute(ctx, rpc, operator, ws, p)

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
		var p protocol.SessionDetachParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		e.detach(p.SID)
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
		if err := gomsf.NewSessionManager(rpc).Stop(ctx, p.SID); err != nil {
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
		if eb := e.sessionUpgrade(ctx, operator, ws, p); eb != nil {
			return nil, eb
		}
		return nil, nil

	case protocol.MethodWorkspaceList:
		names, err := gomsf.NewDbManager(rpc).Workspaces().List(ctx)
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
		// refreshMu spans the whole transition including the workspace RPC:
		// a periodic refresh that already read the old workspace must not
		// commit its stale rows after the clear below, nor interleave its
		// old-workspace page reads with the new workspace's
		e.refreshMu.Lock()
		defer e.refreshMu.Unlock()
		e.mu.Lock()
		startGen := e.gen
		e.mu.Unlock()
		if err := gomsf.NewDbManager(rpc).SetWorkspace(ctx, p.Name); err != nil {
			return nil, mapErr(err)
		}
		// msf already switched: the connection says so and the old
		// workspace's data must not survive a refresh that fails halfway.
		// The generation guard keeps a link that was replaced mid-switch
		// from having its fresh state cleared by this stale command.
		e.mu.Lock()
		if e.gen != startGen {
			e.mu.Unlock()
			return nil, notConnected()
		}
		e.conn.Workspace = p.Name
		e.hosts = nil
		e.services = nil
		e.creds = nil
		e.loot = nil
		e.logfOp(operator, protocol.LevelInfo, "switched to workspace %s", p.Name)
		e.bus.send(protocol.ConnectionUpdate(e.conn))
		e.bus.send(protocol.HostsUpdate(nil))
		e.bus.send(protocol.ServicesUpdate(nil))
		e.bus.send(protocol.CredsUpdate(nil))
		e.bus.send(protocol.LootUpdate(nil))
		e.mu.Unlock()
		// sessions, jobs and routes belong to the framework instance, not
		// the workspace, and stay; a superseded refresh means the link died
		// mid-switch and the teardown rebuilds state on its own
		if err := e.refreshDBLocked(ctx); err != nil && !errors.Is(err, errSuperseded) {
			return nil, mapErr(err)
		}
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
		return e.hailMary(ctx, operator, ws, p)

	case protocol.MethodJobStop:
		var p protocol.JobRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.ID == "" {
			return nil, badParam("id is required")
		}
		if err := gomsf.NewJobManager(rpc).Stop(ctx, p.ID); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "job %s stopped", p.ID)
		return nil, nil

	case protocol.MethodSessionRun:
		var p protocol.SessionRunParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" || p.Command == "" {
			return nil, badParam("sid and command are required")
		}
		return e.sessionRun(ctx, p)

	case protocol.MethodPayloadGenerate:
		var p protocol.PayloadGenerateParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Payload == "" || p.Format == "" {
			return nil, badParam("payload and format are required")
		}
		return e.payloadGenerate(ctx, rpc, p)

	case protocol.MethodHostAdd:
		var p protocol.HostAddParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Address == "" {
			return nil, badParam("address is required")
		}
		opts := map[string]interface{}{"host": p.Address}
		if p.Name != "" {
			opts["name"] = p.Name
		}
		if p.OSName != "" {
			opts["os_name"] = p.OSName
		}
		if p.Comments != "" {
			opts["comments"] = p.Comments
		}
		if _, err := rpc.Call(ctx, gomsf.DbReportHost, opts); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "host %s added", p.Address)
		_ = e.refreshDB(ctx)
		return nil, nil

	case protocol.MethodHostDelete:
		var p protocol.HostRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Address == "" {
			return nil, badParam("address is required")
		}
		if _, err := rpc.Call(ctx, gomsf.DbDelHost, map[string]interface{}{"host": p.Address}); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "host %s deleted", p.Address)
		_ = e.refreshDB(ctx)
		return nil, nil

	case protocol.MethodServiceDelete:
		var p protocol.ServiceRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Host == "" || p.Port == 0 {
			return nil, badParam("host and port are required")
		}
		return e.serviceDelete(ctx, rpc, operator, ws, p)

	case protocol.MethodDBImport:
		var p protocol.DBImportParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if strings.TrimSpace(p.Data) == "" {
			return nil, badParam("data is required")
		}
		if _, err := rpc.Call(ctx, gomsf.DbImportData, map[string]interface{}{"workspace": ws, "data": p.Data}); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelSuccess, "scan data imported")
		_ = e.refreshDB(ctx)
		return nil, nil

	case protocol.MethodHostNote:
		var p protocol.HostNoteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Address == "" {
			return nil, badParam("address is required")
		}
		if _, err := rpc.Call(ctx, gomsf.DbReportHost, map[string]interface{}{"host": p.Address, "comments": p.Comments}); err != nil {
			return nil, mapErr(err)
		}
		_ = e.refreshDB(ctx)
		return nil, nil

	case protocol.MethodCredAdd:
		var p protocol.CredAddParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Host == "" || p.User == "" {
			return nil, badParam("host and user are required")
		}
		return e.credAdd(ctx, rpc, operator, ws, p)

	case protocol.MethodCredDelete:
		var p protocol.CredRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.Host == "" {
			return nil, badParam("host is required")
		}
		return e.credDelete(ctx, rpc, operator, ws, p)

	case protocol.MethodLootDownload:
		var p protocol.LootRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		return e.lootDownload(ctx, rpc, ws, p)
	}
	return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "unreachable dispatch for " + method}
}

func (e *Engine) moduleExecute(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.ModuleExecuteParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	// JSON numbers decode as float64; msf's option validator rejects floats
	// for integer options, so integral values become integers first
	normalizeNumericOptions(p.Options)
	normalizeNumericOptions(p.PayloadOptions)

	mod, err := gomsf.NewModuleWithContext(ctx, rpc, gomsf.ModuleType(p.Type), p.Name)
	if err != nil {
		return nil, mapErr(err)
	}
	for k, v := range p.Options {
		if err := mod.SetOption(k, v); err != nil {
			return nil, mapErr(err)
		}
	}

	// Register before the RPC so sessions and job events that arrive
	// before its response can be attributed to this launch.
	var runKey string
	if p.Type == string(gomsf.ExploitModuleType) && p.Name != "multi/handler" {
		runKey = e.beginExploitRun(p.Type+"/"+p.Name, ws, operator)
	}
	var res *gomsf.ModuleExecuteResult
	if p.Payload != "" {
		payload, perr := gomsf.NewModuleWithContext(ctx, rpc, gomsf.PayloadModuleType, p.Payload)
		if perr != nil {
			if runKey != "" {
				e.forgetExploitRun(runKey)
			}
			return nil, mapErr(perr)
		}
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
	if e.interactBufs == nil {
		e.interactBufs = make(map[string][]byte)
	}
	if _, ok := e.interactBufs[sid]; !ok {
		// first attach starts a fresh transcript; re-attaching keeps the
		// buffered one, which is already consumed from the RPC stream and
		// cannot be polled back
		e.interactBufs[sid] = nil
	}
	e.interactSID = sid
	mon := e.monitor
	// the update carries the buffered transcript: browsers replace their
	// local copy on every interact update and would blank it without this
	e.bus.send(protocol.InteractUpdate(&protocol.InteractState{
		SID: sid, Output: string(e.interactBufs[sid]),
	}))
	e.mu.Unlock()
	if mon != nil {
		mon.WatchSession(sid)
	}
	return nil
}

// detach closes one session console, or the active one when sid is empty.
// Other attached sessions keep their transcripts and streams.
func (e *Engine) detach(sid string) {
	e.mu.Lock()
	if sid == "" {
		sid = e.interactSID
	}
	if sid == "" {
		e.mu.Unlock()
		return
	}
	delete(e.interactBufs, sid)
	wasActive := e.interactSID == sid
	if wasActive {
		e.interactSID = ""
	}
	mon := e.monitor
	if wasActive {
		e.bus.send(protocol.InteractUpdate(&protocol.InteractState{}))
	}
	e.mu.Unlock()
	if mon != nil {
		mon.UnwatchSession(sid)
	}
}

func (e *Engine) sessionWrite(ctx context.Context, p protocol.SessionWriteParams) *protocol.ErrorBody {
	e.mu.Lock()
	rpc := e.rpc
	_, attached := e.interactBufs[p.SID]
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
	prompt := p.SID + " sh > "
	if session.Type == "meterpreter" {
		prompt = "meterpreter " + p.SID + " > "
	}
	echo := prompt + strings.TrimSuffix(p.Data, "\n") + "\n"
	// Session streams do not echo commands. Store the echo in the engine
	// transcript for re-attach and other operators, using the browser's
	// prompt format. The echo must precede the write: the monitor can
	// deliver the command's output while the write is still in flight.
	// A write that fails rolls it back, because the transcript is what a
	// later re-attach replays and a command that never reached the daemon
	// must not become part of the session's history.
	var echoed bool
	e.mu.Lock()
	if _, ok := e.interactBufs[p.SID]; ok {
		e.interactBufs[p.SID] = appendCapped(e.interactBufs[p.SID], []byte(echo))
		echoed = true
		e.bus.send(protocol.SessionOutputMsg{Type: protocol.KindSessionOutput, SID: p.SID, Data: echo})
	}
	e.mu.Unlock()
	var err error
	if session.Type == "meterpreter" {
		err = gomsf.NewMeterpreterSession(rpc, p.SID).Write(ctx, data)
	} else {
		err = gomsf.NewShellSession(rpc, p.SID).Write(ctx, data)
	}
	if err != nil {
		if echoed {
			e.rollbackEcho(p.SID, echo)
		}
		e.logf(protocol.LevelWarn, "session %s write failed: %v", p.SID, err)
		return mapErr(err)
	}
	return nil
}

// rollbackEcho drops the echo of a write that failed from the transcript.
// The line has already gone out on the bus and cannot be recalled without
// either a new protocol message or hijacking every operator's active tab,
// so the failure is reported in the event log and the session history that
// survives a re-attach stays truthful.
func (e *Engine) rollbackEcho(sid, echo string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if buf, ok := e.interactBufs[sid]; ok {
		if trimmed, cut := bytes.CutSuffix(buf, []byte(echo)); cut {
			e.interactBufs[sid] = trimmed
		}
	}
}

func (e *Engine) sessionUpgrade(ctx context.Context, operator, ws string, p protocol.SessionUpgradeParams) *protocol.ErrorBody {
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
	e.eventfOpIn(ws, operator, protocol.LevelSuccess, "session %s upgrading to meterpreter via %s:%d", p.SID, p.LHOST, p.LPORT)
	return nil
}

func (e *Engine) currentConsole() *gomsf.MsfConsole {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.console
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
	case errors.Is(err, gomsf.ErrConsoleNotFound):
		return &protocol.ErrorBody{Code: protocol.CodeUnexpected, Message: err.Error()}
	case errors.Is(err, gomsf.ErrCommandTimeout):
		return &protocol.ErrorBody{Code: protocol.CodeTimeout, Message: err.Error()}
	case errors.Is(err, gomsf.ErrSessionNotFound):
		return &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: err.Error()}
	case errors.Is(err, gomsf.ErrInvalidOption):
		return &protocol.ErrorBody{Code: protocol.CodeInvalidOption, Message: err.Error()}
	case errors.Is(err, gomsf.ErrNotAuthenticated):
		return &protocol.ErrorBody{Code: protocol.CodeNotConnected, Message: err.Error()}
	case errors.Is(err, errConsoleLineBreak):
		return &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: err.Error()}
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
		protocol.MethodAttacksFind, protocol.MethodHailMary,
		protocol.MethodJobStop, protocol.MethodSessionRun, protocol.MethodPayloadGenerate,
		protocol.MethodHostAdd, protocol.MethodHostDelete, protocol.MethodServiceDelete,
		protocol.MethodDBImport, protocol.MethodHostNote, protocol.MethodCredAdd,
		protocol.MethodCredDelete, protocol.MethodLootDownload:
		return true
	}
	return false
}
