package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestServiceToken(t *testing.T) {
	cases := []struct {
		name string
		port int
		want string
	}{
		{"smb", 445, "smb"},
		{"microsoft-ds", 445, "smb"},
		{"https", 443, "http"},
		{"", 3389, "rdp"},
		{"ms-sql-s", 0, "mssql"},
		{"unknown", 0, ""},
		{"tcpwrapped", 0, ""},
		{"", 0, ""},
	}
	for _, c := range cases {
		if got := serviceToken(c.name, c.port); got != c.want {
			t.Errorf("serviceToken(%q, %d) = %q, want %q", c.name, c.port, got, c.want)
		}
	}
}

func TestMatchAttacks(t *testing.T) {
	exploits := []string{
		"windows/smb/ms17_010_eternalblue",
		"multi/http/workspace_one_upload",
		"linux/ssh/fortinet_backdoor",
		"unix/ftp/vsftpd_234_backdoor",
		"windows/rdp/cve_2019_0708_bluekeep",
		"multi/misc/legend_bot_exec",
	}
	services := []*protocol.ServiceState{
		{Host: "10.0.0.5", Port: 445, Proto: "tcp", Name: "smb"},
		{Host: "10.0.0.5", Port: 80, Proto: "tcp", Name: "http"},
	}

	matches := matchAttacks(exploits, services, "Windows 10")
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2: %+v", len(matches), matches)
	}
	if matches[0].Name != "windows/smb/ms17_010_eternalblue" {
		t.Errorf("os-matched exploit should sort first, got %s", matches[0].Name)
	}
	if matches[0].Reason != "service smb · windows" {
		t.Errorf("reason %q", matches[0].Reason)
	}
	if matches[1].Name != "multi/http/workspace_one_upload" || matches[1].Reason != "service http" {
		t.Errorf("second match %+v", matches[1])
	}
}

func TestMatchAttacksNoServices(t *testing.T) {
	if got := matchAttacks([]string{"windows/smb/ms17_010_eternalblue"}, nil, "windows"); got != nil {
		t.Fatalf("expected no matches without services, got %+v", got)
	}
	svc := []*protocol.ServiceState{{Port: 0, Proto: "tcp", Name: "unknown"}}
	if got := matchAttacks([]string{"windows/smb/ms17_010_eternalblue"}, svc, ""); got != nil {
		t.Fatalf("unnamed unknown services should not match, got %+v", got)
	}
}

func TestMatchAttacksCap(t *testing.T) {
	var exploits []string
	for i := 0; i < attackMatchCap+50; i++ {
		exploits = append(exploits, "multi/http/mod"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	matches := matchAttacks(exploits, []*protocol.ServiceState{{Port: 80, Name: "http"}}, "")
	if len(matches) != attackMatchCap {
		t.Fatalf("cap not applied: %d", len(matches))
	}
}

func TestAttacksFindCommand(t *testing.T) {
	fake := stdFake()
	fake.setList(gomsf.ModuleExploits, "modules",
		"windows/smb/ms17_010_eternalblue", "multi/http/test_upload", "linux/local/nothing")
	fake.set(gomsf.DbHosts, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"hosts": []interface{}{map[string]interface{}{
			"host": "10.0.0.5", "address": "10.0.0.5", "os_name": "Windows Server 2016",
		}}}, nil
	})
	fake.set(gomsf.DbServices, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"services": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "port": 445, "proto": "tcp", "name": "smb"},
			map[string]interface{}{"host": "10.0.0.5", "port": 80, "proto": "tcp", "name": "http"},
		}}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "127.0.0.1", Port: 55553, Password: "x"}); err != nil {
		t.Fatal(err)
	}

	raw, eb := e.attacksFind("10.0.0.5")
	if eb != nil {
		t.Fatalf("attacksFind: %v", eb)
	}
	var payload protocol.AttacksPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Host != "10.0.0.5" || len(payload.Matches) != 2 {
		t.Fatalf("payload %+v", payload)
	}
	if payload.Matches[0].Name != "windows/smb/ms17_010_eternalblue" {
		t.Fatalf("ordering %+v", payload.Matches)
	}

	if _, eb := e.attacksFind("10.9.9.9"); eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("unknown host should be bad_params, got %+v", eb)
	}
}
