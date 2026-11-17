//go:build integration

package server

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Full stack against a live msfrpcd: two browser-style sockets join the team
// server, chat crosses between them, and one operator's console stream never
// reaches the other's socket.
func TestIntegrationTeamServerEndToEnd(t *testing.T) {
	if os.Getenv("RUN_MSF_INTEGRATION") != "1" {
		t.Skip("RUN_MSF_INTEGRATION=1 not set")
	}
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "smoketoken", Options{Version: "test", Team: true})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	host := os.Getenv("MSF_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	pass := os.Getenv("MSF_PASSWORD")
	if pass == "" {
		pass = "testpass123"
	}
	if err := e.Connect(context.Background(), protocol.ConnectParams{
		Host: host, Port: 55553, Username: "msf", Password: pass,
	}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	dana := dialWS(t, s, url, "smoketoken")
	mile := dialWS(t, s, url, "smoketoken")
	sendCmd(t, dana, 1, "operator.join", "dana", nil)
	readUntil(t, dana, func(m map[string]any) bool { return m["type"] == "snapshot" })
	sendCmd(t, mile, 1, "operator.join", "mile", nil)
	readUntil(t, mile, func(m map[string]any) bool { return m["type"] == "snapshot" })

	// chat crosses sockets, attributed
	sendCmd(t, dana, 2, "team.chat", "dana", map[string]any{"text": "e2e chat ping"})
	ev := readUntil(t, mile, func(m map[string]any) bool {
		return m["type"] == "event" && m["level"] == "chat"
	})
	if ev["text"] != "e2e chat ping" || ev["operator"] != "dana" {
		t.Fatalf("chat event %+v", ev)
	}

	// console isolation through the real socket filter
	sendCmd(t, dana, 3, "console.write", "dana", map[string]any{"command": "version\n"})
	deadline := time.Now().Add(30 * time.Second)
	for {
		c := e.ConsoleFor("dana")
		if c != nil && strings.Contains(c.Output, "Framework:") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dana's version output never arrived")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// mile's socket must carry none of dana's console stream
	mile.UnderlyingConn().SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		var m map[string]any
		if err := mile.ReadJSON(&m); err != nil {
			break // drained to the deadline
		}
		if m["type"] == "consoleOutput" && strings.Contains(m["data"].(string), "Framework:") {
			t.Fatal("dana's console output reached mile's socket")
		}
		if m["type"] == "resource" && m["resource"] == "console" && m["operator"] == "dana" {
			t.Fatal("dana's console resource reached mile's socket")
		}
	}
}
