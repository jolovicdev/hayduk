package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestSessionUsernameFromInfo(t *testing.T) {
	cases := map[string]string{
		"msfadmin @ sshbox (uid=1000)":          "msfadmin",
		"NT AUTHORITY\\SYSTEM @ WIN7 (uid=0)":   "NT AUTHORITY\\SYSTEM",
		"root @ 10.0.0.5":                       "root",
		"SSH msfadmin @ 172.18.0.3:2222":        "msfadmin",
		"SMB CORP\\alice @ 10.0.0.5":            "CORP\\alice",
		"www-data (uid=33)":                     "www-data",
		"":                                      "",
		"Command shell session 2 opened":        "",
		"meterpreter session started on target": "",
	}
	for info, want := range cases {
		if got := sessionUsername(info, ""); got != want {
			t.Errorf("sessionUsername(%q) = %q, want %q", info, got, want)
		}
	}
}

func TestDaemonUsernamePlaceholderFallsBackToInfo(t *testing.T) {
	s := &gomsf.Session{Username: "unknown", Info: "SSH msfadmin @ 172.18.0.3:2222"}
	if got := daemonSessionUsername(s); got != "msfadmin" {
		t.Fatalf("daemonSessionUsername = %q, want msfadmin", got)
	}
	s.Username = "CORP\\alice"
	if got := daemonSessionUsername(s); got != "CORP\\alice" {
		t.Fatalf("real username overridden: %q", got)
	}
}

func TestSessionTargetHostFallbacks(t *testing.T) {
	cases := []struct {
		name string
		in   *gomsf.Session
		want string
	}{
		{"target wins", &gomsf.Session{TargetHost: "10.0.0.5", SessionHost: "10.0.0.6"}, "10.0.0.5"},
		{"session host", &gomsf.Session{SessionHost: "10.0.0.6"}, "10.0.0.6"},
		{"tunnel peer", &gomsf.Session{TunnelPeer: "10.0.0.5:4444"}, "10.0.0.5"},
		{"composite tunnel peer", &gomsf.Session{TunnelPeer: "10.0.0.1:4444-10.0.0.5:1234"}, "10.0.0.5"},
		{"ipv6 tunnel peer", &gomsf.Session{TunnelPeer: "[2001:db8::5]:4444"}, "2001:db8::5"},
		{"description", &gomsf.Session{Desc: "Command shell session 1 opened (10.0.0.1:4444 -> 10.0.0.7:1234) at x"}, "10.0.0.7"},
	}
	for _, tc := range cases {
		if got := sessionTargetHost(tc.in); got != tc.want {
			t.Errorf("%s: sessionTargetHost = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSessionStateFillsIdentity(t *testing.T) {
	st := sessionState("3", &gomsf.Session{
		Type:       "shell",
		Info:       "msfadmin @ sshbox (uid=1000)",
		TunnelPeer: "172.18.0.1:4444-172.18.0.3:2222",
	}, "default")
	if st.Username != "msfadmin" || st.TargetHost != "172.18.0.3" {
		t.Fatalf("identity missing: %+v", st)
	}
	if st.Workspace != "default" || st.OpenedAt.IsZero() {
		t.Fatalf("attribution fields lost: %+v", st)
	}
}

func TestReconcileSessionsEnrichesLateIdentity(t *testing.T) {
	live := map[string]interface{}{
		"4": map[string]interface{}{
			"type": "shell", "info": "msfadmin @ target (uid=1000)",
			"tunnel_peer": "10.0.0.1:4444-10.0.0.8:4444",
			"target_host": "10.0.0.8",
		},
	}
	var mu sync.Mutex
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		return live, nil
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatal(err)
	}
	// Seed an old row the way an early open event would: no identity yet.
	e.mu.Lock()
	e.sessions["4"] = &protocol.SessionState{ID: "4", Type: "shell", TargetHost: "10.0.0.8"}
	e.mu.Unlock()

	e.reconcileSessions(context.Background())
	st := e.State().Sessions["4"]
	if st == nil || st.Username != "msfadmin" {
		t.Fatalf("username was not enriched: %+v", st)
	}
	if st.TargetHost != "10.0.0.8" {
		t.Fatalf("target host changed: %+v", st)
	}
}
