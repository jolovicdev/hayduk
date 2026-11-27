package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// One-shot work that has no dedicated RPC (payload generation, console-only
// db deletes, credential writes) runs on a temporary utility console: the
// operator's console never sees it, and the route console keeps its job.

// lockedUtilityConsole is utilityConsole with toolsMu held. Every caller that
// runs console-only work goes through it so the unlock happens on every
// path: a panic in the string work below must not leave the mutex locked
// and wedge every later session command.
func (e *Engine) lockedUtilityConsole(ctx context.Context, rpc gomsf.RPCCaller, lines []string) (string, error) {
	return e.lockedUtilityConsoleUntil(ctx, rpc, lines, nil)
}

// lockedUtilityConsoleUntil runs lines on a temporary utility console and, when
// until is non-nil, continues polling after the last command until until
// accepts the accumulated cleaned output (for background jobs like exploit -j
// that print their banner asynchronously after the console prompt goes idle).
func (e *Engine) lockedUtilityConsoleUntil(ctx context.Context, rpc gomsf.RPCCaller, lines []string, until func(string) bool) (string, error) {
	e.toolsMu.Lock()
	defer e.toolsMu.Unlock()
	return e.utilityConsole(ctx, rpc, lines, until)
}

// errConsoleLineBreak rejects a utility-console command whose own text holds
// a line break. The console is line-oriented: quoting a value for Ruby's
// Shellwords keeps it one argv token, but an embedded newline still ends the
// console input line, so the rest of the value would run as a command of its
// own. There is no legitimate console value with a newline, so refuse it
// instead of changing what the operator typed.
var errConsoleLineBreak = errors.New("console command contains a line break")

func (e *Engine) utilityConsole(ctx context.Context, rpc gomsf.RPCCaller, lines []string, until func(string) bool) (string, error) {
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return "", errConsoleLineBreak
		}
	}
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
	poll := e.utilityPoll
	if poll <= 0 {
		poll = min(e.cfg.OutputInterval, 100*time.Millisecond)
	}
	var out strings.Builder
	// A read can report idle before the console has even started on a
	// queued command, so idle only counts after the console was seen busy,
	// produced output and then fell quiet, or stayed idle across several reads.
	drain := func() error {
		sawBusy := false
		sawData := false
		idleReads := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			result, err := mc.Read(ctx)
			if err != nil {
				return err
			}
			if result.Data != "" {
				out.WriteString(result.Data)
				sawData = true
			}
			if result.Busy {
				sawBusy = true
				idleReads = 0
				continue
			}
			idleReads++
			if sawBusy || (sawData && result.Data == "") || idleReads >= 6 {
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
	if until != nil {
		waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
		defer waitCancel()
		for !until(cleanOutput(out.String())) {
			select {
			case <-waitCtx.Done():
				return cleanOutput(out.String()), nil
			case <-time.After(poll):
			}
			result, err := mc.Read(waitCtx)
			if err != nil {
				if waitCtx.Err() != nil && ctx.Err() == nil {
					return cleanOutput(out.String()), nil
				}
				return "", err
			}
			if result.Data != "" {
				out.WriteString(result.Data)
			}
		}
	}
	return cleanOutput(out.String()), nil
}

// sessionRun runs one meterpreter command and returns its output. The
// session is unwatched for the duration: the monitor and RunSingle would
// otherwise race as two readers on one stream.
func (e *Engine) sessionRun(ctx context.Context, p protocol.SessionRunParams) (json.RawMessage, *protocol.ErrorBody) {
	ms, done, eb := e.beginTool(p.SID)
	if eb != nil {
		return nil, eb
	}
	defer done()
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := ms.RunSingle(rctx, p.Command)
	if err != nil {
		return nil, mapErr(err)
	}
	return mustJSON(protocol.SessionRunPayload{Output: cleanOutput(out)}), nil
}

// Payload generation runs directly through msfrpcd's module.execute on the
// payload module, which returns the generated artifact bytes in the RPC reply.
// That lets browsers download generated payloads straight to the operator's
// machine even when msfrpcd runs on a remote host.
var payloadFormats = map[string]bool{
	"base64": true, "hex": true, "c": true, "csharp": true, "java": true,
	"python": true, "powershell": true, "ps1": true, "ruby": true, "sh": true,
	"exe": true, "elf": true, "macho": true, "msi": true,
	"apk": true, "war": true, "jar": true, "raw": true,
}

var textPayloadFormats = map[string]bool{
	"base64": true, "hex": true, "c": true, "csharp": true, "java": true,
	"python": true, "powershell": true, "ps1": true, "ruby": true, "sh": true,
}

var payloadFormatExt = map[string]string{
	"powershell": "ps1", "python": "py", "ruby": "rb", "csharp": "cs",
	"raw": "bin", "base64": "b64", "hex": "hex",
}

func (e *Engine) payloadGenerate(ctx context.Context, rpc gomsf.RPCCaller, p protocol.PayloadGenerateParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	if !payloadFormats[p.Format] {
		return nil, badParam("unsupported format: " + p.Format)
	}
	if strings.ContainsAny(p.Payload, " \t\r\n") {
		return nil, badParam("invalid payload name")
	}
	normalizeNumericOptions(p.Options)
	opts := make(map[string]interface{}, len(p.Options)+3)
	for k, v := range p.Options {
		if !optionNameRe.MatchString(k) {
			return nil, badParam("invalid payload option name: " + k)
		}
		if s, ok := v.(string); ok && strings.ContainsAny(s, "\r\n") {
			return nil, badParam(errConsoleLineBreak.Error())
		}
		opts[k] = v
	}
	opts["Format"] = p.Format
	if enc := strings.TrimSpace(p.Encoder); enc != "" {
		if strings.ContainsAny(enc, " \t\r\n") {
			return nil, badParam("invalid encoder name")
		}
		opts["Encoder"] = enc
	}
	if p.Iterations > 0 {
		opts["Iterations"] = p.Iterations
	}
	res, err := gomsf.NewModuleManager(rpc).Execute(ctx, gomsf.PayloadModuleType, p.Payload, opts)
	if err != nil {
		return nil, mapErr(err)
	}
	if res == nil || len(res.Payload) == 0 {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "payload generation produced empty output"}
	}
	rawBytes := []byte(res.Payload)
	out := protocol.PayloadGeneratePayload{
		Filename:   payloadFilename(p.Payload, p.Format),
		DataBase64: base64.StdEncoding.EncodeToString(rawBytes),
		Size:       len(rawBytes),
	}
	if textPayloadFormats[p.Format] {
		out.Text = res.Payload
	}
	return mustJSON(out), nil
}

func payloadBase(payload string) string {
	if i := strings.LastIndex(payload, "/"); i >= 0 {
		return payload[i+1:]
	}
	return payload
}

func payloadFilename(payload, format string) string {
	ext := format
	if mapped, ok := payloadFormatExt[format]; ok {
		ext = mapped
	}
	return fmt.Sprintf("%s.%s", payloadBase(payload), ext)
}

// services -d selects on host and port alone. Metasploit's services command
// parses -r/--protocol but applies it to the add and update paths only, so a
// delete that carries it drops the filter before the search and takes every
// protocol on that port with it. The UI deletes row by row, so a tcp/udp pair
// on one port would silently lose the row the operator did not click. Refuse
// instead: an over-deletion they cannot see is worse than one they must
// resolve at the daemon.
func (e *Engine) serviceProtoConflict(host string, port int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	protos := map[string]bool{}
	for _, s := range e.services {
		if s != nil && s.Host == host && s.Port == port && s.Proto != "" {
			protos[s.Proto] = true
		}
	}
	if len(protos) < 2 {
		return ""
	}
	names := make([]string, 0, len(protos))
	for p := range protos {
		names = append(names, p)
	}
	sort.Strings(names)
	return fmt.Sprintf(
		"%s:%d is recorded on %s, and msfconsole cannot delete one protocol at a time",
		host, port, strings.Join(names, " and "))
}

func (e *Engine) serviceDelete(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.ServiceRefParams) (json.RawMessage, *protocol.ErrorBody) {
	if conflict := e.serviceProtoConflict(p.Host, p.Port); conflict != "" {
		return nil, badParam(conflict)
	}
	out, err := e.lockedUtilityConsole(ctx, rpc, []string{
		fmt.Sprintf("services -d -p %d %s", p.Port, consoleArg(p.Host)),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Deleted 0 services") {
		return nil, badParam(fmt.Sprintf("no service %s:%d to delete", p.Host, p.Port))
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "service %s:%d deleted", p.Host, p.Port)
	e.requestRefresh()
	return nil, nil
}

// consoleArg quotes one value for the msfconsole line it lands in.
// msfconsole splits its arguments with Ruby's shellwords, where a single
// quote protects everything up to the next quote, so an embedded quote is
// escaped the POSIX way - close, backslash-escape, reopen. Deleting it
// instead would write a different credential than the operator typed and
// report success, which is worse than a rejected add. A bare double quote
// or backslash starts a quoted or escaped run too, so either one forces the
// whole value inside quotes.
func consoleArg(v string) string {
	if v == "" {
		return v
	}
	if !strings.ContainsAny(v, " \t'\"\\") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// optionNameRe constrains the option names that reach a console line. msf
// option names are identifiers, so anything else - a newline above all -
// can only be an attempt to smuggle a second console command into the
// utility console's script.
var optionNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// consoleOptionArg renders one option value for `set K V`. Scalars print
// bare: quoting a bool or an integer would hand msfconsole a literal that
// its option validator rejects. A string goes through consoleArg so a value
// carrying a space - a User-Agent, a custom command - survives, and one
// carrying a newline cannot break out into a console line of its own.
func consoleOptionArg(v interface{}) string {
	switch t := v.(type) {
	case string:
		return consoleArg(t)
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", v)
	default:
		return consoleArg(fmt.Sprintf("%v", v))
	}
}

func (e *Engine) credAdd(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.CredAddParams) (json.RawMessage, *protocol.ErrorBody) {
	parts := []string{"creds add", "address:" + consoleArg(p.Host)}
	if p.Port > 0 {
		parts = append(parts, fmt.Sprintf("port:%d", p.Port), "protocol:tcp")
	}
	if p.Service != "" {
		parts = append(parts, "service-name:"+consoleArg(p.Service))
	}
	parts = append(parts, "user:"+consoleArg(p.User))
	if p.Pass != "" {
		parts = append(parts, "password:"+consoleArg(p.Pass))
	}
	out, err := e.lockedUtilityConsole(ctx, rpc, []string{strings.Join(parts, " ")})
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Failed to add") || strings.Contains(out, "Unknown key") {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "msfrpcd rejected the credential"}
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "credential for %s on %s added", p.User, p.Host)
	e.requestRefresh()
	return nil, nil
}

func (e *Engine) credDelete(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.CredRefParams) (json.RawMessage, *protocol.ErrorBody) {
	parts := []string{"creds -d", consoleArg(p.Host)}
	if p.Port > 0 {
		parts = append(parts, fmt.Sprintf("-p %d", p.Port))
	}
	if p.User != "" {
		parts = append(parts, "-u "+consoleArg(p.User))
	}
	out, err := e.lockedUtilityConsole(ctx, rpc, []string{strings.Join(parts, " ")})
	if err != nil {
		return nil, mapErr(err)
	}
	if strings.Contains(out, "Deleted 0 creds") {
		return nil, badParam("no matching credential to delete")
	}
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "credential for %s deleted", p.Host)
	e.requestRefresh()
	return nil, nil
}

// lootDownload reads through the connection and workspace the dispatcher
// already captured when it cleared the auth gate. Both belong to that
// decision, so neither is re-read here: a workspace.set landing mid-command
// would otherwise pair the old connection with the newer workspace.
func (e *Engine) lootDownload(ctx context.Context, rpc gomsf.RPCCaller, ws string, p protocol.LootRefParams) (json.RawMessage, *protocol.ErrorBody) {
	if rpc == nil {
		return nil, notConnected()
	}
	if p.Host == "" {
		return nil, badParam("host is required")
	}
	db := gomsf.NewDbManager(rpc)
	// db.loots applies its options as record-column filters. Narrow by type
	// and name server-side (host is an association, not a column), then page
	// the remainder. An unpaged read is capped at 100 rows, which used to make
	// a real loot entry look missing. Stop at the first match so a hit early
	// in a large workspace never downloads every payload.
	for offset := 0; ; {
		opts := map[string]interface{}{
			"workspace": ws,
			"limit":     dbPageSize,
			"offset":    offset,
		}
		if p.Type != "" {
			opts["ltype"] = p.Type
		}
		if p.Name != "" {
			opts["name"] = p.Name
		}
		loots, err := db.Loots(ctx, opts)
		if err != nil {
			return nil, mapErr(err)
		}
		for _, l := range loots {
			if l != nil && l.Host == p.Host && l.Type == p.Type && l.Name == p.Name {
				return mustJSON(protocol.LootPayload{Data: l.Data}), nil
			}
		}
		if len(loots) < dbPageSize {
			break
		}
		if offset >= dbMaxRows {
			return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "loot collection exceeds the paging cap"}
		}
		offset += dbPageSize
	}
	return nil, badParam("no such loot entry")
}
