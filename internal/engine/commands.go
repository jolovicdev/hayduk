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
		oc, eb := e.consoleFor(ctx, operator)
		if eb != nil {
			return nil, eb
		}
		if err := oc.console.Write(ctx, p.Command); err != nil {
			return nil, mapErr(err)
		}
		// Readiness is restored only when a read issued after this write
		// reports the console idle (consoleRead checks the generation):
		// the buffered prompt must not go out here, or browsers accept
		// input while msf still runs the command.
		e.mu.Lock()
		if e.consoles[operator] == oc {
			// msfrpcd's web consoles do not echo commands. Store the echo
			// in the engine buffer so the operator's browsers receive it
			// on replay. Reuse the trailing idle prompt to avoid
			// displaying it twice.
			echo := oc.prompt + strings.TrimRight(p.Command, "\n") + "\n"
			if oc.prompt != "" && bytes.HasSuffix(oc.out, []byte(oc.prompt)) {
				oc.out = oc.out[:len(oc.out)-len(oc.prompt)]
			}
			oc.out = appendCapped(oc.out, []byte(echo))
			e.bus.send(protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Operator: operator, Data: echo})
			oc.writePending = true
			oc.writeGen++
		}
		e.mu.Unlock()
		return nil, nil

	case protocol.MethodConsoleTabs:
		var p protocol.ConsoleTabsParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		oc, eb := e.consoleFor(ctx, operator)
		if eb != nil {
			return nil, eb
		}
		tabs, err := oc.console.Tabs(ctx, p.Line)
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
		return e.loadModuleMeta(ctx, rpc, p.Type, p.Name, false)

	case protocol.MethodModuleOptions:
		var p protocol.ModuleRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		return e.loadModuleMeta(ctx, rpc, p.Type, p.Name, true)

	case protocol.MethodPayloads:
		var p protocol.ModuleRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		key := p.Type + "/" + p.Name
		if cached := e.cachedModuleData(&e.modPayloadsCache, key); cached != nil {
			return cached, nil
		}
		payloads, err := gomsf.NewModuleManager(rpc).CompatiblePayloads(ctx, p.Name)
		if err != nil {
			return nil, mapErr(err)
		}
		raw := mustJSON(payloads)
		e.storeModuleData(&e.modPayloadsCache, rpc, key, raw)
		return raw, nil

	case protocol.MethodModuleExecute:
		var p protocol.ModuleExecuteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		return e.moduleExecute(ctx, rpc, operator, ws, p)

	case protocol.MethodModuleCheck:
		var p protocol.ModuleCheckParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if eb := requireModuleRef(p.Type, p.Name); eb != nil {
			return nil, eb
		}
		return e.moduleCheck(ctx, rpc, operator, ws, p)

	case protocol.MethodSessionAttach:
		var p protocol.SessionRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		if eb := e.attach(operator, p.SID); eb != nil {
			return nil, eb
		}
		return nil, nil

	case protocol.MethodSessionDetach:
		var p protocol.SessionDetachParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		e.detach(operator, p.SID)
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

	case protocol.MethodWorkspaceAdd:
		var p protocol.WorkspaceSetParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		name := strings.TrimSpace(p.Name)
		if !validWorkspaceName(name) {
			return nil, badParam("workspace name must be 1-64 printable characters without control characters")
		}
		if err := gomsf.NewDbManager(rpc).Workspaces().Add(ctx, name); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "workspace %s created", name)
		return nil, nil

	case protocol.MethodWorkspaceDelete:
		var p protocol.WorkspaceSetParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, badParam("name is required")
		}
		if name == "default" {
			return nil, badParam("cannot delete the default workspace")
		}
		if name == ws {
			return nil, badParam("switch away from workspace " + name + " before deleting it")
		}
		if err := gomsf.NewDbManager(rpc).Workspaces().Remove(ctx, name); err != nil {
			return nil, mapErr(err)
		}
		e.eventfOpIn(ws, operator, protocol.LevelInfo, "workspace %s deleted", name)
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

	case protocol.MethodSessionFsList:
		var p protocol.SessionFsListParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		return e.sessionFsList(ctx, p)

	case protocol.MethodSessionFileDownload:
		var p protocol.SessionFileRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		return e.sessionFileDownload(ctx, p)

	case protocol.MethodSessionFileUpload:
		var p protocol.SessionFileUploadParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		return e.sessionFileUpload(ctx, p)

	case protocol.MethodSessionScreenshot:
		var p protocol.SessionFileRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		return e.sessionScreenshot(ctx, p)

	case protocol.MethodSessionPost:
		var p protocol.SessionPostParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" || p.Module == "" {
			return nil, badParam("sid and module are required")
		}
		return e.sessionPost(ctx, p)

	case protocol.MethodSessionPostModules:
		var p protocol.SessionRefParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		if p.SID == "" {
			return nil, badParam("sid is required")
		}
		return e.sessionPostModules(ctx, rpc, p.SID)

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
		e.requestRefresh()
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
		e.requestRefresh()
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
		e.requestRefresh()
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
		e.requestRefresh()
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

	case protocol.MethodServicesScan:
		var p protocol.ServiceScanParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		return e.servicesScan(ctx, operator, ws, p)

	case protocol.MethodBruteStart:
		var p protocol.BruteParams
		if eb := parseParams(params, &p); eb != nil {
			return nil, eb
		}
		return e.bruteStart(operator, ws, p)

	case protocol.MethodOperationsCancel:
		return e.operationsCancel(params)
	}
	return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "unreachable dispatch for " + method}
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

func validWorkspaceName(name string) bool {
	if name == "" || len([]rune(name)) > 64 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func knownMethod(method string) bool {
	switch method {
	case protocol.MethodConsoleWrite, protocol.MethodConsoleTabs,
		protocol.MethodModuleInfo, protocol.MethodModuleOptions, protocol.MethodPayloads,
		protocol.MethodModuleExecute, protocol.MethodModuleCheck,
		protocol.MethodSessionAttach, protocol.MethodSessionDetach,
		protocol.MethodSessionWrite, protocol.MethodSessionStop, protocol.MethodSessionUpgrade,
		protocol.MethodWorkspaceList, protocol.MethodWorkspaceSet,
		protocol.MethodWorkspaceAdd, protocol.MethodWorkspaceDelete, protocol.MethodDBRefresh,
		protocol.MethodAttacksFind, protocol.MethodHailMary,
		protocol.MethodJobStop, protocol.MethodSessionRun, protocol.MethodPayloadGenerate,
		protocol.MethodSessionFsList, protocol.MethodSessionFileDownload,
		protocol.MethodSessionFileUpload, protocol.MethodSessionScreenshot,
		protocol.MethodSessionPost, protocol.MethodSessionPostModules,
		protocol.MethodHostAdd, protocol.MethodHostDelete, protocol.MethodServiceDelete,
		protocol.MethodDBImport, protocol.MethodHostNote, protocol.MethodCredAdd,
		protocol.MethodCredDelete, protocol.MethodLootDownload,
		protocol.MethodServicesScan, protocol.MethodBruteStart,
		protocol.MethodOperationsCancel:
		return true
	}
	return false
}
