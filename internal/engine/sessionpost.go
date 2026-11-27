package engine

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Post modules launched from a session run through the session channel
// (`run post/...`) instead of module.execute: their output streams into the
// interact transcript where the operator already is, while database side
// effects (creds, loot, routes) land as usual.

// postModuleRe accepts framework post module paths: lowercase identifiers
// separated by slashes. Anything else could smuggle console arguments.
var postModuleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_/]*$`)

func (e *Engine) sessionPost(ctx context.Context, p protocol.SessionPostParams) (json.RawMessage, *protocol.ErrorBody) {
	name := strings.TrimPrefix(strings.TrimSpace(p.Module), "post/")
	if !postModuleRe.MatchString(name) {
		return nil, badParam("invalid post module name: " + p.Module)
	}
	line := "run post/" + name
	keys := make([]string, 0, len(p.Options))
	for k := range p.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !optionNameRe.MatchString(k) {
			return nil, badParam("invalid option name: " + k)
		}
		if s, ok := p.Options[k].(string); ok && strings.ContainsAny(s, "\r\n") {
			return nil, badParam(errConsoleLineBreak.Error())
		}
		line += " " + k + "=" + consoleOptionArg(p.Options[k])
	}
	if eb := e.sessionWrite(ctx, protocol.SessionWriteParams{SID: p.SID, Data: line}); eb != nil {
		return nil, eb
	}
	return mustJSON(protocol.SessionPostPayload{Command: line}), nil
}

// sessionPostModules lists the post modules the daemon considers compatible
// with the session. When the daemon cannot answer (older or minimal RPC),
// the full post index is the fallback so the picker still opens.
func (e *Engine) sessionPostModules(ctx context.Context, rpc gomsf.RPCCaller, sid string) (json.RawMessage, *protocol.ErrorBody) {
	mods, err := gomsf.NewSessionManager(rpc).CompatibleModules(ctx, sid)
	if err != nil {
		e.mu.Lock()
		if e.modules != nil {
			mods = append([]string(nil), e.modules.Post...)
		}
		e.mu.Unlock()
	}
	normalized := make([]string, 0, len(mods))
	for _, m := range mods {
		normalized = append(normalized, strings.TrimPrefix(m, "post/"))
	}
	sort.Strings(normalized)
	return mustJSON(protocol.SessionPostModulesPayload{Modules: normalized}), nil
}
