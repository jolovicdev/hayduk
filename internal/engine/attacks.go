package engine

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// The matcher is deliberately honest and dumb: an exploit is offered when its
// refname contains the path token of a service the host runs (windows/smb/...
// for smb). It knows nothing about versions or patch levels; the dialog copy
// says as much.

const attackMatchCap = 200

// serviceAliases normalize database service names that do not carry their
// protocol in the string itself.
var serviceAliases = map[string]string{
	"microsoft-ds":   "smb",
	"netbios-ssn":    "smb",
	"ms-wbt-server":  "rdp",
	"ms-sql-s":       "mssql",
	"ms-sql-m":       "mssql",
	"http-proxy":     "http",
	"https-alt":      "http",
	"domain":         "dns",
	"postgresql":     "postgres",
	"imaps":          "imap",
	"pop3s":          "pop3",
	"ssl/mysql":      "mysql",
}

var portServices = map[int]string{
	139: "smb", 445: "smb",
	80: "http", 443: "http", 591: "http", 8000: "http", 8080: "http", 8443: "http",
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp",
	53: "dns", 88: "kerberos", 161: "snmp", 389: "ldap",
	143: "imap", 110: "pop3", 993: "imap", 995: "pop3",
	1433: "mssql", 3306: "mysql", 3389: "rdp", 5432: "postgres",
	5900: "vnc", 6379: "redis", 2049: "nfs", 27017: "mongodb",
}

// serviceToken normalizes a database service to the token module paths use.
func serviceToken(name string, port int) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return portServices[port]
	}
	if alias, ok := serviceAliases[name]; ok {
		return alias
	}
	if strings.Contains(name, "http") {
		return "http"
	}
	if strings.Contains(name, "smb") {
		return "smb"
	}
	if name == "unknown" || name == "tcpwrapped" || name == "service" {
		return ""
	}
	return name
}

func osPathPrefix(osName string) string {
	osName = strings.ToLower(osName)
	switch {
	case strings.Contains(osName, "windows"):
		return "windows"
	case strings.Contains(osName, "linux"):
		return "linux"
	case strings.Contains(osName, "unix"):
		return "unix"
	}
	return ""
}

// serviceOpen keeps rows whose state says the port is reachable. An empty
// state counts: not every scanner records one.
func serviceOpen(s *protocol.ServiceState) bool {
	return s.State == "" || s.State == "open"
}

func matchAttacks(exploits []string, services []*protocol.ServiceState, osName string) []protocol.AttackMatch {
	// tokenPorts maps every service token to the port it was struck on; a
	// token's presence is the match set, and the lowest port wins so the
	// choice never depends on database row order (0 = token without a port).
	tokenPorts := make(map[string]int)
	for _, s := range services {
		if s == nil || !serviceOpen(s) {
			continue
		}
		if t := serviceToken(s.Name, s.Port); t != "" {
			if prev, ok := tokenPorts[t]; !ok || (s.Port > 0 && (prev == 0 || s.Port < prev)) {
				tokenPorts[t] = s.Port
			}
		}
	}
	if len(tokenPorts) == 0 {
		return nil
	}
	prefix := osPathPrefix(osName)

	type ranked struct {
		match   protocol.AttackMatch
		osFirst bool
	}
	var found []ranked
	for _, name := range exploits {
		lower := strings.ToLower(name)
		token := matchToken(lower, tokenPorts)
		if token == "" {
			continue
		}
		reason := "service " + token
		osFirst := prefix != "" && strings.HasPrefix(lower, prefix+"/")
		if osFirst {
			reason += " · " + prefix
		}
		found = append(found, ranked{protocol.AttackMatch{Name: name, Reason: reason, Port: tokenPorts[token]}, osFirst})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].osFirst != found[j].osFirst {
			return found[i].osFirst
		}
		return found[i].match.Name < found[j].match.Name
	})
	matches := make([]protocol.AttackMatch, 0, len(found))
	for _, f := range found {
		matches = append(matches, f.match)
	}
	if len(matches) > attackMatchCap {
		matches = matches[:attackMatchCap]
	}
	return matches
}

// matchToken picks the service token an exploit refname matches on. A token
// that IS one of the path segments wins - linux/http/x is an http module
// even when "ssh" appears later in its name - and the lexicographically
// first substring match otherwise, so the same module never resolves to a
// different service between runs.
func matchToken(lower string, tokenPorts map[string]int) string {
	for _, seg := range strings.Split(lower, "/") {
		if _, ok := tokenPorts[seg]; ok {
			return seg
		}
	}
	best := ""
	for t := range tokenPorts {
		if strings.Contains(lower, t) && (best == "" || t < best) {
			best = t
		}
	}
	return best
}

func (e *Engine) attacksFind(host string) (json.RawMessage, *protocol.ErrorBody) {
	e.mu.Lock()
	var hs *protocol.HostState
	for _, h := range e.hosts {
		if h != nil && h.Address == host {
			hs = h
			break
		}
	}
	var svcs []*protocol.ServiceState
	for _, s := range e.services {
		if s != nil && s.Host == host {
			svcs = append(svcs, s)
		}
	}
	var exploits []string
	if e.modules != nil {
		exploits = e.modules.Exploits
	}
	e.mu.Unlock()

	if hs == nil {
		return nil, &protocol.ErrorBody{Code: protocol.CodeBadParams, Message: "no such host: " + host}
	}
	return mustJSON(protocol.AttacksPayload{Host: host, Matches: matchAttacks(exploits, svcs, hs.OSName)}), nil
}
