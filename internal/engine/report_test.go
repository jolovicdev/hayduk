package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
