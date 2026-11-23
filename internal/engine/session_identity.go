package engine

import (
	"net"
	"net/netip"
	"regexp"
	"strings"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Metasploit puts the login identity in session.info for most session types
// and leaves username empty; host fields are similarly inconsistent between
// shell, meterpreter and handler-opened sessions. These helpers recover the
// values the daemon did provide instead of showing "unknown" in the UI.

var (
	sessionUserAtRe = regexp.MustCompile(`^\s*(.{1,128}?)\s+@\s+\S`)
	sessionUserUID  = regexp.MustCompile(`^\s*([^\s()]{1,128})\s*\(uid=`)
	sessionDescPeer = regexp.MustCompile(`(?i)->\s*\[?([0-9a-f:.]+)\]?:\d+`)
)

func sessionUsername(info, desc string) string {
	for _, text := range []string{info, desc} {
		if m := sessionUserAtRe.FindStringSubmatch(text); len(m) == 2 {
			if user := cleanSessionUser(stripSessionProtocol(m[1])); user != "" {
				return user
			}
		}
		if m := sessionUserUID.FindStringSubmatch(text); len(m) == 2 {
			if user := cleanSessionUser(m[1]); user != "" {
				return user
			}
		}
	}
	return ""
}

// Session info from the login scanners often reads "SSH msfadmin @ host" or
// "SMB CORP\\alice @ host". The protocol label is not part of the identity.
var sessionProtocolLabels = map[string]bool{
	"ssh": true, "smb": true, "ftp": true, "telnet": true, "mysql": true,
	"postgresql": true, "postgres": true, "mssql": true, "vnc": true,
	"rdp": true, "shell": true, "meterpreter": true,
}

func stripSessionProtocol(user string) string {
	fields := strings.Fields(strings.TrimSpace(user))
	if len(fields) < 2 {
		return user
	}
	label := strings.ToLower(strings.TrimSuffix(fields[0], ":"))
	if !sessionProtocolLabels[label] {
		return user
	}
	return strings.Join(fields[1:], " ")
}

func cleanSessionUser(user string) string {
	user = strings.TrimSpace(user)
	if user == "" || len(user) > 128 || placeholderSessionUser(user) {
		return ""
	}
	lower := strings.ToLower(user)
	// Sentences from a session description are not identities.
	for _, noise := range []string{"session ", "opened ", "closed ", "meterpreter ", "command shell"} {
		if strings.Contains(lower, noise) {
			return ""
		}
	}
	return user
}

// placeholderSessionUser names values the daemon uses when it has no
// identity, so they never mask a username recoverable from info.
func placeholderSessionUser(user string) bool {
	switch strings.ToLower(strings.TrimSpace(user)) {
	case "", "unknown", "n/a", "none", "-":
		return true
	}
	return false
}

// daemonSessionUsername prefers a real username field, then the identity in
// session.info. A daemon that reports "unknown" in username does not win over
// a parsed "msfadmin".
func daemonSessionUsername(s *gomsf.Session) string {
	if s == nil {
		return ""
	}
	if user := strings.TrimSpace(s.Username); !placeholderSessionUser(user) {
		return user
	}
	return sessionUsername(s.Info, s.Desc)
}

// sessionTargetHost prefers the daemon's explicit target, then the session
// host, then the remote side of the tunnel/description. This is the host the
// topology and inspector should attribute the session to.
func sessionTargetHost(s *gomsf.Session) string {
	if s == nil {
		return ""
	}
	if host := cleanSessionHost(s.TargetHost); host != "" {
		return host
	}
	if host := cleanSessionHost(s.SessionHost); host != "" {
		return host
	}
	if host := tunnelPeerHost(s.TunnelPeer); host != "" {
		return host
	}
	if m := sessionDescPeer.FindStringSubmatch(s.Desc); len(m) == 2 {
		return cleanSessionHost(m[1])
	}
	return ""
}

func tunnelPeerHost(peer string) string {
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return ""
	}
	// Some daemons report "local:port-remote:port" instead of a single
	// host:port. The remote side is the last segment.
	if i := strings.LastIndex(peer, "->"); i >= 0 {
		peer = strings.TrimSpace(peer[i+2:])
	} else if strings.Count(peer, "-") == 1 {
		peer = strings.TrimSpace(peer[strings.LastIndex(peer, "-")+1:])
	}
	if host, _, err := net.SplitHostPort(peer); err == nil {
		return cleanSessionHost(host)
	}
	if ip, err := netip.ParseAddr(strings.Trim(peer, "[]")); err == nil {
		return ip.String()
	}
	return ""
}

func cleanSessionHost(host string) string {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || len(host) > 253 {
		return ""
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String()
	}
	if validScanHostname(host) {
		return host
	}
	return ""
}

// enrichSessionState fills identity fields that were absent when a session
// first appeared. It never changes workspace attribution or opened time.
func enrichSessionState(st *protocol.SessionState, s *gomsf.Session) bool {
	if st == nil || s == nil {
		return false
	}
	changed := false
	set := func(dst *string, value string) {
		value = strings.TrimSpace(value)
		if value == "" || *dst == value {
			return
		}
		*dst = value
		changed = true
	}
	// Only replace the user when the daemon now knows it; a blank poll must
	// not erase an identity recovered from info.
	if user := daemonSessionUsername(s); user != "" {
		set(&st.Username, user)
	}
	if st.TargetHost == "" {
		set(&st.TargetHost, sessionTargetHost(s))
	}
	set(&st.Type, s.Type)
	set(&st.TunnelPeer, s.TunnelPeer)
	set(&st.ViaExploit, s.ViaExploit)
	set(&st.ViaPayload, s.ViaPayload)
	set(&st.Info, s.Info)
	set(&st.SessionHost, cleanSessionHost(s.SessionHost))
	set(&st.UUID, s.UUID)
	return changed
}
