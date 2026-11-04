package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Service discovery runs through Metasploit's db_nmap helper rather than a
// portscan module: db_nmap both fingerprints the services and imports them
// into the workspace database, so the Services table gets names and versions
// in one pass. It runs on its own console so a long nmap invocation never
// blocks the operator's console or the route poller.
const (
	serviceScanTopPorts = "1000"
	// Hard bounds keep a wedged nmap from holding the scan slot forever and
	// keep a large range from filling engine memory with console output.
	serviceScanMaxDuration = 30 * time.Minute
	serviceScanMaxOutput   = 1 << 20
	serviceScanStartGrace  = 30 * time.Second
	serviceScanConsoleWait = 5 * time.Second
)

var scanPortPieceRe = regexp.MustCompile(`^[0-9]+(?:-[0-9]+)?$`)

// normalizeScanTargets validates the target list before it is interpolated
// into a console command. nmap accepts names and addresses, but a console
// line also accepts shell syntax, so only address/CIDR/hostname tokens pass.
func normalizeScanTargets(raw string) (string, error) {
	fields := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	if len(fields) == 0 {
		return "", errors.New("target is required")
	}
	if len(fields) > 64 {
		return "", errors.New("too many targets (maximum 64)")
	}
	for _, target := range fields {
		if strings.HasPrefix(target, "-") || len(target) > 253 {
			return "", fmt.Errorf("invalid target %q", target)
		}
		if _, err := netip.ParseAddr(target); err == nil {
			continue
		}
		if strings.Contains(target, "/") {
			if _, err := netip.ParsePrefix(target); err != nil {
				return "", fmt.Errorf("invalid target %q", target)
			}
			continue
		}
		if !validScanHostname(target) {
			return "", fmt.Errorf("invalid target %q", target)
		}
	}
	return strings.Join(fields, " "), nil
}

// validScanHostname is deliberately narrower than DNS: letters, digits,
// hyphens and dots, with no empty label and no leading/trailing hyphen.
func validScanHostname(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
			if !alnum && c != '-' {
				return false
			}
			if c == '-' && (i == 0 || i == len(label)-1) {
				return false
			}
		}
	}
	return true
}

// normalizeScanPorts accepts comma-separated ports and ranges, the syntax
// nmap's -p option uses. It normalizes whitespace and validates every bound
// before the value reaches the console.
func normalizeScanPorts(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 512 {
		return "", errors.New("too many port ranges (maximum 512)")
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !scanPortPieceRe.MatchString(part) {
			return "", fmt.Errorf("invalid port list %q", raw)
		}
		loText, hiText, isRange := strings.Cut(part, "-")
		lo, err := strconv.Atoi(loText)
		if err != nil || lo < 1 || lo > 65535 {
			return "", fmt.Errorf("invalid port %q", loText)
		}
		if isRange {
			hi, err := strconv.Atoi(hiText)
			if err != nil || hi < lo || hi > 65535 {
				return "", fmt.Errorf("invalid port range %q", part)
			}
			out = append(out, strconv.Itoa(lo)+"-"+strconv.Itoa(hi))
			continue
		}
		out = append(out, strconv.Itoa(lo))
	}
	return strings.Join(out, ","), nil
}

// serviceScanCommand builds the console line. Every interpolated value has
// already passed the validators above.
func serviceScanCommand(target, ports string, skipDiscovery bool) string {
	args := []string{"db_nmap", "-sV", "--version-light", "-T4", "--open"}
	if ports != "" {
		args = append(args, "-p", ports)
	} else {
		args = append(args, "--top-ports", serviceScanTopPorts)
	}
	if skipDiscovery {
		args = append(args, "-Pn")
	}
	args = append(args, target)
	return strings.Join(args, " ")
}

// openPortCount counts the "22/tcp open ssh" lines in db_nmap's console
// output. It is only used for the completion event, so a conservative
// pattern is preferable to guessing at a version string.
var openPortRe = regexp.MustCompile(`(?m)(?:^|[\s:])([0-9]{1,5})/(tcp|udp)\s+open\b`)

func openPortCount(output string) int {
	return len(openPortRe.FindAllStringSubmatchIndex(output, -1))
}

// serviceScanFailure turns the common db_nmap prerequisites into an
// actionable error. A scan that simply finds nothing is not an error.
func serviceScanFailure(output string) string {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "database not connected"),
		strings.Contains(lower, "database isn't connected"),
		strings.Contains(lower, "database is not connected"):
		return "Metasploit's database is not connected; connect it before scanning services"
	case strings.Contains(lower, "could not find nmap"),
		strings.Contains(lower, "nmap: not found"),
		strings.Contains(lower, "nmap not found"),
		strings.Contains(lower, "nmap is not installed"),
		strings.Contains(lower, "no such file or directory") && strings.Contains(lower, "nmap"):
		return "nmap is not installed on the Metasploit host; install nmap or use a portscan module"
	case strings.Contains(lower, "unknown command: db_nmap"),
		strings.Contains(lower, "unknown command db_nmap"):
		return "this Metasploit build has no db_nmap command"
	case (strings.Contains(lower, "[-] error") || strings.Contains(lower, "[-] unknown command")) &&
		!strings.Contains(lower, "nmap done"):
		return "db_nmap reported an error; check the target and the Metasploit console"
	}
	return ""
}

// servicesScan starts an asynchronous version scan as an engine operation.
// The command returns immediately so a slow scan does not hold the operator's
// command slot; completion lands in the operation, the event log and the
// Services resource. The scan is anchored to the engine's run context, not the
// websocket command context, so closing the browser does not abort it.
func (e *Engine) servicesScan(_ context.Context, operator, ws string, p protocol.ServiceScanParams) (json.RawMessage, *protocol.ErrorBody) {
	target, err := normalizeScanTargets(p.Target)
	if err != nil {
		return nil, badParam(err.Error())
	}
	ports, err := normalizeScanPorts(p.Ports)
	if err != nil {
		return nil, badParam(err.Error())
	}
	rpc := e.connectedRPC()
	if rpc == nil {
		return nil, notConnected()
	}
	id, ctx, eb := e.beginOperation(protocol.OpScan, operator, ws, target, 0, 0)
	if eb != nil {
		return nil, eb
	}
	go e.runServiceScan(ctx, rpc, id, operator, ws, target, ports, p.SkipDiscovery)
	return mustJSON(protocol.ServiceScanPayload{Target: target, Ports: ports, OperationID: id}), nil
}

func (e *Engine) runServiceScan(ctx context.Context, rpc gomsf.RPCCaller, opID, operator, ws, target, ports string, skipDiscovery bool) {
	status, summary := protocol.OpFailed, "service scan failed"
	defer func() { e.finishOperation(opID, status, summary) }()

	command := serviceScanCommand(target, ports, skipDiscovery)
	e.eventfOpIn(ws, operator, protocol.LevelInfo, "service scan started on %s", target)

	output, err := e.runScanConsole(ctx, rpc, command)
	if err != nil {
		if ctx.Err() != nil {
			status = protocol.OpCanceled
			summary = "service scan canceled"
			return
		}
		summary = "service scan failed: " + err.Error()
		e.eventfOpIn(ws, operator, protocol.LevelError, "service scan failed: %v", err)
		return
	}
	if failure := serviceScanFailure(output); failure != "" {
		summary = "service scan failed: " + failure
		e.eventfOpIn(ws, operator, protocol.LevelError, "service scan failed: %s", failure)
		return
	}

	before := e.serviceCount()
	if err := e.refreshDB(ctx); err != nil {
		if ctx.Err() != nil {
			status = protocol.OpCanceled
			summary = "service scan canceled"
			return
		}
		summary = "service scan finished, but the workspace refresh failed: " + err.Error()
		e.eventfOpIn(ws, operator, protocol.LevelWarn, "service scan finished, but the workspace refresh failed: %v", err)
		return
	}
	after := e.serviceCount()
	found := openPortCount(output)
	status = protocol.OpDone
	switch {
	case found == 0:
		summary = "no open ports found; check the target and port range"
		e.eventfOpIn(ws, operator, protocol.LevelWarn,
			"service scan finished on %s: no open ports found; check the target and port range", target)
	case after > before:
		summary = fmt.Sprintf("%d open port(s), %d new service row(s)", found, after-before)
		e.eventfOpIn(ws, operator, protocol.LevelSuccess,
			"service scan finished on %s: %d open port(s) detected, %d new service row(s)", target, found, after-before)
	default:
		summary = fmt.Sprintf("%d open port(s) detected", found)
		e.eventfOpIn(ws, operator, protocol.LevelSuccess,
			"service scan finished on %s: %d open port(s) detected", target, found)
	}
}

func (e *Engine) serviceCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.services)
}

// runScanConsole creates a disposable console, drains its banner, runs the
// command, and reads until the prompt settles. The command owns the console
// for its whole run, so no operator output is interleaved and no toolsMu is
// needed; session-tool serialization protects a different stream.
func (e *Engine) runScanConsole(ctx context.Context, rpc gomsf.RPCCaller, command string) (string, error) {
	cm := gomsf.NewConsoleManager(rpc)
	con, err := cm.Create(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), serviceScanConsoleWait)
		defer cancel()
		_ = cm.Destroy(dctx, con.ID)
	}()
	console := gomsf.NewMsfConsole(rpc, con.ID)
	// The new console prints its banner asynchronously. Drain it before the
	// write so the completion heuristic sees only the scan's output.
	if err := e.drainScanConsole(ctx, console); err != nil {
		return "", err
	}
	if err := console.Write(ctx, command+"\n"); err != nil {
		return "", err
	}

	var out strings.Builder
	started := time.Now()
	idle := 0
	startedScan := false
	for {
		if time.Since(started) > serviceScanMaxDuration {
			return out.String(), fmt.Errorf("scan exceeded %s", serviceScanMaxDuration)
		}
		select {
		case <-ctx.Done():
			return out.String(), ctx.Err()
		case <-time.After(min(e.cfg.OutputInterval, 200*time.Millisecond)):
		}
		result, err := console.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return out.String(), ctx.Err()
			}
			return out.String(), err
		}
		appendScanOutput(&out, result.Data)
		if result.Data != "" && !startedScan && scanOutputStarted(out.String()) {
			startedScan = true
		}
		if result.Busy {
			startedScan = true
			idle = 0
			continue
		}
		if result.Data != "" {
			idle = 0
			continue
		}
		// A newly written command can sit in the queue while the console
		// reports idle; don't declare completion during that start window.
		if !startedScan {
			if time.Since(started) < serviceScanStartGrace {
				continue
			}
			return out.String(), errors.New("scan command produced no output; check that nmap is available on the Metasploit host")
		}
		idle++
		if idle >= 3 {
			return out.String(), nil
		}
	}
}

// drainScanConsole consumes the new console's banner before the command is
// written, so a late banner cannot be mistaken for scan output.
func (e *Engine) drainScanConsole(ctx context.Context, console *gomsf.MsfConsole) error {
	quiet := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(e.cfg.OutputInterval, 100*time.Millisecond)):
		}
		result, err := console.Read(ctx)
		if err != nil {
			return err
		}
		if result.Busy || result.Data != "" {
			quiet = 0
			continue
		}
		quiet++
		if quiet >= 3 {
			return nil
		}
	}
}

// scanOutputStarted separates the command's first real output from a late
// console banner. nmap always announces itself; the diagnostics path covers
// a missing nmap/database before any scan line appears.
func scanOutputStarted(data string) bool {
	lower := strings.ToLower(data)
	if strings.Contains(lower, "starting nmap") ||
		strings.Contains(lower, "nmap scan report") ||
		strings.Contains(lower, "nmap done") {
		return true
	}
	return serviceScanFailure(data) != ""
}

func appendScanOutput(out *strings.Builder, data string) {
	if out.Len() >= serviceScanMaxOutput || data == "" {
		return
	}
	if remaining := serviceScanMaxOutput - out.Len(); len(data) > remaining {
		data = data[:remaining]
	}
	out.WriteString(data)
}
