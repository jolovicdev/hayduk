package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// One-shot work that has no dedicated RPC (payload generation, console-only
// db deletes, credential writes) runs on a temporary utility console: the
// operator's console never sees it, and the route console keeps its job.

func (e *Engine) utilityConsole(ctx context.Context, rpc gomsf.RPCCaller, lines []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cm := gomsf.NewConsoleManager(rpc)
	con, err := cm.Create(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_ = cm.Destroy(dctx, con.ID)
	}()
	mc := gomsf.NewMsfConsole(rpc, con.ID)
	var out strings.Builder
	// A read can report idle before the console has even started on a
	// queued command, so idle only counts after the console was seen busy
	// or several consecutive idle reads produced nothing new.
	drain := func() error {
		sawBusy := false
		idleReads := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(min(e.cfg.OutputInterval, 200*time.Millisecond)):
			}
			result, err := mc.Read(ctx)
			if err != nil {
				return err
			}
			out.WriteString(result.Data)
			if result.Busy {
				sawBusy = true
				idleReads = 0
				continue
			}
			idleReads++
			if sawBusy || idleReads >= 6 {
				return nil
			}
		}
	}
	if err := drain(); err != nil { // banner
		return "", err
	}
	for _, line := range lines {
		if err := mc.Write(ctx, line+"\n"); err != nil {
			return "", err
		}
		if err := drain(); err != nil {
			return "", err
		}
	}
	return cleanOutput(out.String()), nil
}

// sessionRun runs one meterpreter command and returns its output. The
// session is unwatched for the duration: the monitor and RunSingle would
// otherwise race as two readers on one stream.
func (e *Engine) sessionRun(ctx context.Context, p protocol.SessionRunParams) (json.RawMessage, *protocol.ErrorBody) {
	e.toolsMu.Lock()
	defer e.toolsMu.Unlock()
	e.mu.Lock()
	rpc := e.rpc
	session := e.sessions[p.SID]
	_, attached := e.interactBufs[p.SID]
	mon := e.monitor
	e.mu.Unlock()
	if rpc == nil {
		return nil, notConnected()
	}
	if session == nil {
		return nil, &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + p.SID}
	}
	if session.Type != "meterpreter" {
		return nil, badParam("session tools need a meterpreter session; use Interact for shells")
	}
	if attached && mon != nil {
		mon.UnwatchSession(p.SID)
		defer mon.WatchSession(p.SID)
	}
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := gomsf.NewMeterpreterSession(rpc, p.SID).RunSingle(rctx, p.Command)
	if err != nil {
		return nil, mapErr(err)
	}
	return mustJSON(protocol.SessionRunPayload{Output: cleanOutput(out)}), nil
}

// Payload generation writes to a file on the msfrpcd host for every
// format: over RPC a payload printed to the console never reaches the
// console reader, so claiming a browser download would be a lie. The
// operator copies the file from the reported path.
var payloadFormats = map[string]bool{
	"base64": true, "hex": true, "c": true, "csharp": true, "java": true,
	"python": true, "powershell": true, "ps1": true, "ruby": true, "sh": true,
	"exe": true, "elf": true, "macho": true, "msi": true,
	"apk": true, "war": true, "jar": true, "raw": true,
}

func (e *Engine) payloadGenerate(ctx context.Context, rpc gomsf.RPCCaller, p protocol.PayloadGenerateParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	if !payloadFormats[p.Format] {
		return nil, badParam("unsupported format: " + p.Format)
	}
	e.toolsMu.Lock()
	defer e.toolsMu.Unlock()
	normalizeNumericOptions(p.Options)
	lines := []string{"use payload/" + p.Payload}
	keys := make([]string, 0, len(p.Options))
	for k := range p.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("set %s %v", k, p.Options[k]))
	}
	base := p.Payload
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	saved := "/tmp/hayduk-" + base + "." + p.Format
	lines = append(lines, "generate -f "+p.Format+" -o "+saved)
	out, err := e.utilityConsole(ctx, rpc, lines)
	if err != nil {
		return nil, mapErr(err)
	}
	if !strings.Contains(out, "Writing") || !strings.Contains(out, saved) {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "payload generation produced no file"}
	}
	return mustJSON(protocol.PayloadGeneratePayload{
		Filename: base + "." + p.Format, SavedPath: saved,
	}), nil
}

func (e *Engine) serviceDelete(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.ServiceRefParams) (json.RawMessage, *protocol.ErrorBody) {
	e.toolsMu.Lock()
	out, err := e.utilityConsole(ctx, rpc, []string{
		fmt.Sprintf("services -d -p %d %s", p.Port, p.Host),
	})
	e.toolsMu.Unlock()
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Deleted 0 services") {
		return nil, badParam(fmt.Sprintf("no service %s:%d to delete", p.Host, p.Port))
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "service %s:%d deleted", p.Host, p.Port)
	_ = e.refreshDB(ctx)
	return nil, nil
}

func credConsoleArg(v string) string {
	if v == "" {
		return v
	}
	if strings.ContainsAny(v, " \t'") {
		return "'" + strings.ReplaceAll(v, "'", "") + "'"
	}
	return v
}

func (e *Engine) credAdd(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.CredAddParams) (json.RawMessage, *protocol.ErrorBody) {
	parts := []string{"creds add", "address:" + credConsoleArg(p.Host)}
	if p.Port > 0 {
		parts = append(parts, fmt.Sprintf("port:%d", p.Port), "protocol:tcp")
	}
	if p.Service != "" {
		parts = append(parts, "service-name:"+credConsoleArg(p.Service))
	}
	parts = append(parts, "user:"+credConsoleArg(p.User))
	if p.Pass != "" {
		parts = append(parts, "password:"+credConsoleArg(p.Pass))
	}
	e.toolsMu.Lock()
	out, err := e.utilityConsole(ctx, rpc, []string{strings.Join(parts, " ")})
	e.toolsMu.Unlock()
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Failed to add") || strings.Contains(out, "Unknown key") {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "msfrpcd rejected the credential"}
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "credential for %s on %s added", p.User, p.Host)
	_ = e.refreshDB(ctx)
	return nil, nil
}

func (e *Engine) credDelete(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.CredRefParams) (json.RawMessage, *protocol.ErrorBody) {
	parts := []string{"creds -d", p.Host}
	if p.Port > 0 {
		parts = append(parts, fmt.Sprintf("-p %d", p.Port))
	}
	if p.User != "" {
		parts = append(parts, "-u "+credConsoleArg(p.User))
	}
	e.toolsMu.Lock()
	out, err := e.utilityConsole(ctx, rpc, []string{strings.Join(parts, " ")})
	e.toolsMu.Unlock()
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Deleted 0 creds") {
		return nil, badParam("no matching credential to delete")
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "credential for %s deleted", p.Host)
	_ = e.refreshDB(ctx)
	return nil, nil
}

func (e *Engine) lootDownload(ctx context.Context, rpc gomsf.RPCCaller, p protocol.LootRefParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	_, ws := e.dispatchScope()
	loots, err := gomsf.NewDbManager(rpc).Loots(ctx, map[string]interface{}{"workspace": ws})
	if err != nil {
		return nil, mapErr(err)
	}
	for _, l := range loots {
		if l != nil && l.Host == p.Host && l.Type == p.Type && l.Name == p.Name {
			return mustJSON(protocol.LootPayload{Data: l.Data}), nil
		}
	}
	return nil, badParam("no such loot entry")
}

