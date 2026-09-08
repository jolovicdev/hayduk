package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestReportEscapesHostileStrings(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	e.mu.Lock()
	e.conn = protocol.ConnectionState{Status: "connected", Workspace: "labs", MSFVersion: "6.5.2"}
	e.hosts = []*protocol.HostState{{
		Address: "10.0.0.5", Name: "<script>alert(1)</script>", OSName: "Windows", OSFlavor: "Server 2016",
	}}
	e.services = []*protocol.ServiceState{{Host: "10.0.0.5", Port: 445, Proto: "tcp", Name: "smb", Info: `<img src=x onerror="pwn()">`}}
	e.creds = []*protocol.CredState{{Host: "10.0.0.5", Port: 445, Service: "smb", User: "admin", Pass: "s3cret&#39;", Type: "password"}}
	e.events = []*protocol.EventEntry{{Seq: 1, Time: time.Now().UTC(), Level: "info", Text: "discovered host <b>10.0.0.5</b>"}}
	e.mu.Unlock()

	html, eb := e.reportDocument()
	if eb != nil {
		t.Fatalf("reportDocument: %+v", eb)
	}
	for _, want := range []string{"&lt;script&gt;", "&lt;img src=x", "&lt;b&gt;10.0.0.5&lt;/b&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("report does not contain escaped %q", want)
		}
	}
	for _, banned := range []string{"<script>alert", "<img src=x", "<b>10.0.0.5"} {
		if strings.Contains(html, banned) {
			t.Errorf("report contains raw %q", banned)
		}
	}
	for _, want := range []string{"campaign report", "workspace <b>labs</b>", "Credentials", "sensitive", "For authorized security testing only"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if !strings.Contains(html, "s3cret&amp;#39;") {
		t.Errorf("credential material must round-trip escaped")
	}
}

func TestReportCommandWorksDisconnected(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	raw, eb := e.Exec(nil, "", protocol.MethodReportHTML, nil)
	if eb != nil {
		t.Fatalf("report.html: %+v", eb)
	}
	var payload protocol.ReportPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.HTML, "No hosts recorded") {
		t.Fatal("empty campaign should still render a document")
	}
}

func TestReportRetainsOperator(t *testing.T) {
	e := testEngine(t)
	e.eventfOp("operator-47", protocol.LevelInfo, "campaign event")
	doc, err := e.reportDocument()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "operator-47") {
		t.Fatal("report dropped operator attribution")
	}
}

// A report for one workspace excludes another workspace's events.
func TestReportWorkspaceIsolation(t *testing.T) {
	f := stdFake()
	f.set(gomsf.DbSetWorkspace, func(...interface{}) (interface{}, error) {
		return map[string]interface{}{"result": "success"}, nil
	})
	f.set(gomsf.DbCurrentWorkspace, func(...interface{}) (interface{}, error) {
		return map[string]interface{}{"workspace": "client-beta"}, nil
	})
	e := testEngine(t)
	e.rpc = f
	e.conn.Workspace = "client-alpha"
	e.eventf(protocol.LevelInfo, "discovered host CLIENT_ALPHA_PRIVATE_HOST")
	if _, err := e.Exec(context.Background(), "", protocol.MethodWorkspaceSet, json.RawMessage(`{"name":"client-beta"}`)); err != nil {
		t.Fatal(err)
	}
	doc, err := e.reportDocument()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "CLIENT_ALPHA_PRIVATE_HOST") {
		t.Fatal("client-beta report carries a client-alpha event")
	}
}

// Sessions are framework-wide; the report keeps the ones opened under the
// exported workspace. Untagged sessions fall back to host membership.
func TestReportSessionsScopedToWorkspace(t *testing.T) {
	e := testEngine(t)
	e.conn.Workspace = "client-beta"
	e.hosts = []*protocol.HostState{{Address: "10.0.0.9"}}
	e.sessions = map[string]*protocol.SessionState{
		// opened under client-alpha: excluded even though the same host
		// exists in both workspaces
		"1": {ID: "1", Type: "shell", TargetHost: "10.0.0.9", Workspace: "client-alpha",
			Username: "alpha-user", ViaExploit: "exploit/multi/alpha"},
		"2": {ID: "2", Type: "shell", TargetHost: "10.0.0.9", Workspace: "client-beta",
			Username: "beta-user"},
		// no workspace tag: host membership decides
		"3": {ID: "3", Type: "shell", TargetHost: "10.0.0.9", Username: "legacy-user"},
		"4": {ID: "4", Type: "shell", TargetHost: "10.0.0.1", Username: "outsider"},
	}
	doc, err := e.reportDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"beta-user", "legacy-user"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("session %s missing from its own workspace report", want)
		}
	}
	for _, leak := range []string{"alpha-user", "exploit/multi/alpha", "outsider"} {
		if strings.Contains(doc, leak) {
			t.Fatalf("report leaks a session from another workspace: %s", leak)
		}
	}
}
