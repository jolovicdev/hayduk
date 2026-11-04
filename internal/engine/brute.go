package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Login scanners take one credential pair per run, so a brute-force spray is
// a paced sequence of module.execute calls. The browser used to drive it; the
// engine does now, which keeps the run alive across a reload, makes it
// visible to the team, and gives operators a real cancel.
const (
	bruteAttemptCap = 250
	bruteWorkers    = 3
	brutePace       = 100 * time.Millisecond
	bruteMaxHosts   = 256
	bruteMaxList    = 512
	bruteMaxValue   = 512
)

// bruteModuleRe restricts the operation to login scanners. module.execute is
// already available to every operator, so this is not a security boundary; it
// keeps a mistyped or hostile params blob from turning the spray into a
// generic launcher.
var bruteModuleRe = regexp.MustCompile(`^scanner/(?:[a-z0-9_]+/)+[a-z0-9_]*_login$`)

type bruteAttempt struct {
	host string
	user string
	pass string
	port int
}

type brutePlan struct {
	module   string
	userless bool
	attempts []bruteAttempt
	capped   int
}

// buildBrutePlan validates the dialog input and expands the cartesian
// product, mirroring the browser's preview rules. The engine's plan is the
// authoritative one; the UI preview only sizes the button.
func buildBrutePlan(p protocol.BruteParams) (brutePlan, error) {
	module := strings.TrimSpace(p.Module)
	if !bruteModuleRe.MatchString(module) {
		return brutePlan{}, fmt.Errorf("module must be a login scanner refname, got %q", p.Module)
	}
	hosts, err := cleanBruteValues(p.Hosts, bruteMaxHosts, 253)
	if err != nil {
		return brutePlan{}, fmt.Errorf("hosts: %w", err)
	}
	if len(hosts) == 0 {
		return brutePlan{}, fmt.Errorf("at least one host is required")
	}
	for _, host := range hosts {
		if !validBruteHost(host) {
			return brutePlan{}, fmt.Errorf("invalid host %q", host)
		}
	}
	users, err := cleanBruteValues(p.Users, bruteMaxList, bruteMaxValue)
	if err != nil {
		return brutePlan{}, fmt.Errorf("users: %w", err)
	}
	passwords, err := cleanBruteValues(p.Passwords, bruteMaxList, bruteMaxValue)
	if err != nil {
		return brutePlan{}, fmt.Errorf("passwords: %w", err)
	}
	if !p.Userless && len(users) == 0 {
		return brutePlan{}, fmt.Errorf("at least one username is required")
	}
	if p.Port != 0 && !validPort(p.Port) {
		return brutePlan{}, fmt.Errorf("port must be between 1 and 65535")
	}
	ports := make(map[string]int, len(p.Ports))
	for host, port := range p.Ports {
		if port != 0 && !validPort(port) {
			return brutePlan{}, fmt.Errorf("invalid port %d for %s", port, host)
		}
		ports[host] = port
	}

	// A blank password is one entry in the pass list; a user-as-pass guess
	// depends on the user, so it is added per pair below.
	passes := append([]string(nil), passwords...)
	if p.AddBlank && !containsString(passes, "") {
		passes = append(passes, "")
	}
	if !p.Userless && len(passes) == 0 {
		return brutePlan{}, fmt.Errorf("at least one password (or a blank guess) is required")
	}
	if p.Userless && len(passes) == 0 {
		return brutePlan{}, fmt.Errorf("at least one password is required")
	}

	plan := brutePlan{module: module, userless: p.Userless}
	push := func(host, user, pass string) {
		if len(plan.attempts) >= bruteAttemptCap {
			plan.capped++
			return
		}
		port := p.Port
		if port == 0 {
			port = ports[host]
		}
		plan.attempts = append(plan.attempts, bruteAttempt{host: host, user: user, pass: pass, port: port})
	}
	attemptUsers := users
	if p.Userless {
		attemptUsers = []string{""}
	}
	for _, host := range hosts {
		for _, user := range attemptUsers {
			for _, pass := range passes {
				push(host, user, pass)
			}
			if p.AddUserAsPass && user != "" && !containsString(passes, user) {
				push(host, user, user)
			}
		}
	}
	if len(plan.attempts) == 0 {
		return brutePlan{}, fmt.Errorf("the plan has no attempts")
	}
	return plan, nil
}

// cleanBruteValues trims, drops empties and dedupes while preserving order.
// Values end up inside module options, so a newline or NUL is refused rather
// than silently changed.
func cleanBruteValues(in []string, max, maxLen int) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if len(v) > maxLen {
			return nil, fmt.Errorf("value is longer than %d bytes", maxLen)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, fmt.Errorf("value contains a control character")
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) > max {
			return nil, fmt.Errorf("more than %d values", max)
		}
	}
	return out, nil
}

func validBruteHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	return validScanHostname(host) || isIPAddress(host)
}

func isIPAddress(s string) bool {
	// netip parsing is stricter than the hostname validator and accepts IPv6
	_, err := netip.ParseAddr(s)
	return err == nil
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// bruteAttemptOptions sends only the options every login scanner carries;
// msf validates each option name and rejects extras.
func bruteAttemptOptions(a bruteAttempt, userless bool) map[string]interface{} {
	options := map[string]interface{}{
		"RHOSTS":   a.host,
		"THREADS":  1,
		"PASSWORD": a.pass,
	}
	if a.port > 0 {
		options["RPORT"] = a.port
	}
	if !userless {
		options["USERNAME"] = a.user
	}
	return options
}

func (e *Engine) bruteStart(operator, ws string, p protocol.BruteParams) (json.RawMessage, *protocol.ErrorBody) {
	plan, err := buildBrutePlan(p)
	if err != nil {
		return nil, badParam(err.Error())
	}
	rpc := e.connectedRPC()
	if rpc == nil {
		return nil, notConnected()
	}
	target := p.Hosts[0]
	if len(p.Hosts) > 1 {
		target = fmt.Sprintf("%d hosts", len(p.Hosts))
	}
	id, ctx, eb := e.beginOperation(protocol.OpBrute, operator, ws, target, 0, len(plan.attempts))
	if eb != nil {
		return nil, eb
	}
	go e.runBrute(ctx, rpc, id, operator, ws, plan)
	return mustJSON(protocol.BrutePayload{
		OperationID: id, Planned: len(plan.attempts), Capped: plan.capped,
	}), nil
}

func (e *Engine) runBrute(ctx context.Context, rpc gomsf.RPCCaller, opID, operator, ws string, plan brutePlan) {
	status, summary := protocol.OpFailed, "brute force failed"
	defer func() { e.finishOperation(opID, status, summary) }()

	credsBefore, sessionsBefore := e.campaignCounts()
	e.eventfOpIn(ws, operator, protocol.LevelInfo,
		"brute force started: %d attempt(s) with %s", len(plan.attempts), plan.module)

	var next, done, failures atomic.Int64
	workers := min(bruteWorkers, len(plan.attempts))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				i := int(next.Add(1)) - 1
				if i >= len(plan.attempts) {
					return
				}
				a := plan.attempts[i]
				if _, eb := e.moduleExecute(ctx, rpc, operator, ws, protocol.ModuleExecuteParams{
					Type: "auxiliary", Name: plan.module,
					Options: bruteAttemptOptions(a, plan.userless),
				}); eb != nil {
					failures.Add(1)
					if ctx.Err() != nil {
						return
					}
				}
				n := int(done.Add(1))
				e.advanceOperation(opID, n, bruteProgress(a, plan.userless))
				select {
				case <-ctx.Done():
					return
				case <-time.After(brutePace):
				}
			}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		status = protocol.OpCanceled
		summary = fmt.Sprintf("brute force canceled after %d of %d attempts", done.Load(), len(plan.attempts))
		e.eventfOpIn(ws, operator, protocol.LevelWarn, "%s", summary)
		return
	}

	// The daemon stores successful logins and any sessions asynchronously;
	// one refresh at the end gives the operation an accurate summary.
	if err := e.refreshDB(ctx); err != nil {
		e.eventfOpIn(ws, operator, protocol.LevelWarn, "brute force finished, but the workspace refresh failed: %v", err)
	}
	credsAfter, sessionsAfter := e.campaignCounts()
	status = protocol.OpDone
	summary = fmt.Sprintf("%d attempt(s), %d failed, %d new credential(s), %d new session(s)",
		done.Load(), failures.Load(), credsAfter-credsBefore, sessionsAfter-sessionsBefore)
	e.eventfOpIn(ws, operator, protocol.LevelSuccess, "brute force finished: %s", summary)
}

func bruteProgress(a bruteAttempt, userless bool) string {
	who := a.user
	if userless || who == "" {
		who = "(password)"
	}
	if a.port > 0 {
		return fmt.Sprintf("%s @ %s:%d", who, a.host, a.port)
	}
	return fmt.Sprintf("%s @ %s", who, a.host)
}

func (e *Engine) campaignCounts() (creds, sessions int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.creds), len(e.sessions)
}
