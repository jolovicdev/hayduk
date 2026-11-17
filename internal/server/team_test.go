package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func testTeamServer(t *testing.T) (*Server, string) {
	t.Helper()
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "testtoken", Options{Version: "test", Team: true})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func sendCmd(t *testing.T, c *websocket.Conn, id int64, method, operator string, params any) {
	t.Helper()
	m := map[string]any{"type": "command", "id": id, "method": method}
	if operator != "" {
		m["operator"] = operator
	}
	if params != nil {
		m["params"] = params
	}
	if err := c.WriteJSON(m); err != nil {
		t.Fatal(err)
	}
}

// readUntil pulls messages until one matches, failing on timeout. The
// handshake and presence traffic in between is skipped.
func readUntil(t *testing.T, c *websocket.Conn, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("message never arrived")
		}
		c.UnderlyingConn().SetReadDeadline(deadline)
		m := readMsg(t, c)
		if match(m) {
			return m
		}
	}
}

func isResponse(m map[string]any, id int64) bool {
	return m["type"] == "response" && int64(m["id"].(float64)) == id
}

// In team mode the snapshot waits for the join so it can carry the joined
// operator's personal state; commands without an identity are refused.
func TestTeamSnapshotWaitsForJoin(t *testing.T) {
	s, url := testTeamServer(t)
	c := dialWS(t, s, url, "testtoken")

	hello := readMsg(t, c)
	if hello["type"] != "hello" || hello["team"] != true {
		t.Fatalf("hello %+v", hello)
	}

	// no identity yet: a command must be refused, not run anonymously
	sendCmd(t, c, 1, "console.write", "", map[string]any{"command": "version\n"})
	resp := readUntil(t, c, func(m map[string]any) bool { return isResponse(m, 1) })
	if resp["ok"] != false {
		t.Fatalf("anonymous team command accepted: %+v", resp)
	}

	sendCmd(t, c, 2, "operator.join", "dana", nil)
	snap := readUntil(t, c, func(m map[string]any) bool { return m["type"] == "snapshot" })
	if snap["operator"] != "dana" {
		t.Fatalf("snapshot not scoped to the joined operator: %+v", snap)
	}
	resp = readUntil(t, c, func(m map[string]any) bool { return isResponse(m, 2) })
	if resp["ok"] != true {
		t.Fatalf("join refused: %+v", resp)
	}
	if got := s.engine.State().Operators; len(got) != 1 || got[0] != "dana" {
		t.Fatalf("presence %+v", got)
	}
}

// Chat and presence reach every operator; personal resources do not cross.
func TestTeamChatAndPresenceBroadcast(t *testing.T) {
	s, url := testTeamServer(t)
	dana := dialWS(t, s, url, "testtoken")
	mile := dialWS(t, s, url, "testtoken")

	sendCmd(t, dana, 1, "operator.join", "dana", nil)
	readUntil(t, dana, func(m map[string]any) bool { return m["type"] == "snapshot" })
	sendCmd(t, mile, 1, "operator.join", "mile", nil)
	mileSnap := readUntil(t, mile, func(m map[string]any) bool { return m["type"] == "snapshot" })

	// mile's snapshot already carries the full presence list; dana hears
	// the second join as a resource update
	ops, _ := mileSnap["state"].(map[string]any)["operators"].([]any)
	if len(ops) != 2 {
		t.Fatalf("mile snapshot presence %+v", ops)
	}
	readUntil(t, dana, func(m map[string]any) bool {
		if m["type"] != "resource" || m["resource"] != "operators" {
			return false
		}
		ops, _ := m["operators"].([]any)
		return len(ops) == 2
	})

	sendCmd(t, dana, 2, "team.chat", "dana", map[string]any{"text": "pivot is up"})
	for _, c := range []*websocket.Conn{dana, mile} {
		ev := readUntil(t, c, func(m map[string]any) bool {
			return m["type"] == "event" && m["level"] == "chat"
		})
		if ev["operator"] != "dana" || ev["text"] != "pivot is up" {
			t.Fatalf("chat event %+v", ev)
		}
	}
}

// A rename moves presence and hands the socket the new name's personal
// state; the old name lingers out of the operator list at once.
func TestTeamRenameMovesPresence(t *testing.T) {
	s, url := testTeamServer(t)
	c := dialWS(t, s, url, "testtoken")
	sendCmd(t, c, 1, "operator.join", "dana", nil)
	readUntil(t, c, func(m map[string]any) bool { return m["type"] == "snapshot" })

	sendCmd(t, c, 2, "operator.join", "dana2", nil)
	// the rename delivers the new presence list and the new name's
	// personal resources; their relative order is not contractual
	sawOps, sawConsole := false, false
	deadline := time.Now().Add(5 * time.Second)
	for !(sawOps && sawConsole) {
		if time.Now().After(deadline) {
			t.Fatalf("rename traffic incomplete: ops=%v console=%v", sawOps, sawConsole)
		}
		c.UnderlyingConn().SetReadDeadline(deadline)
		m := readMsg(t, c)
		if m["type"] == "resource" && m["resource"] == "operators" {
			if ops, _ := m["operators"].([]any); len(ops) == 1 && ops[0] == "dana2" {
				sawOps = true
			}
		}
		if m["type"] == "resource" && m["resource"] == "console" && m["operator"] == "dana2" {
			sawConsole = true
		}
	}
	if got := s.engine.State().Operators; len(got) != 1 || got[0] != "dana2" {
		t.Fatalf("presence after rename %+v", got)
	}
}

func TestTeamJoinValidation(t *testing.T) {
	s, url := testTeamServer(t)
	c := dialWS(t, s, url, "testtoken")
	readMsg(t, c) // hello

	for i, name := range []string{"", "   ", strings.Repeat("x", 33), "bad\nname"} {
		id := int64(i + 1)
		sendCmd(t, c, id, "operator.join", name, nil)
		resp := readUntil(t, c, func(m map[string]any) bool { return isResponse(m, id) })
		if resp["ok"] != false {
			t.Fatalf("invalid name %q accepted: %+v", name, resp)
		}
	}
	if got := s.engine.State().Operators; len(got) != 0 {
		t.Fatalf("invalid joins registered presence: %+v", got)
	}
}

// The pump's scope filter: shared resources pass for everyone, personal
// ones reach their owner alone.
func TestMessageForScoping(t *testing.T) {
	shared := []protocol.ServerMessage{
		protocol.EventMsg{Type: protocol.KindEvent, Text: "x"},
		protocol.SessionOutputMsg{Type: protocol.KindSessionOutput, SID: "1", Data: "x"},
		protocol.HostsUpdate(nil),
		protocol.SessionsUpdate(nil),
		protocol.OperatorsUpdate([]string{"dana"}),
		protocol.ConnectionUpdate(protocol.ConnectionState{}),
	}
	for _, m := range shared {
		if !messageFor("dana", m) || !messageFor("", m) {
			t.Fatalf("shared message filtered: %#v", m)
		}
	}
	personal := []protocol.ServerMessage{
		protocol.NewSnapshotFor("dana", protocol.CampaignState{}),
		protocol.ConsoleOutputMsg{Type: protocol.KindConsoleOutput, Operator: "dana", Data: "x"},
		protocol.ConsoleUpdate("dana", &protocol.ConsoleState{}),
		protocol.InteractUpdate("dana", &protocol.InteractState{SID: "1"}),
	}
	for _, m := range personal {
		if !messageFor("dana", m) {
			t.Fatalf("owner filtered out: %#v", m)
		}
		if messageFor("mile", m) || messageFor("", m) {
			t.Fatalf("personal message leaked: %#v", m)
		}
	}
	// the local "" view reaches single-mode sockets only
	local := protocol.NewSnapshot(protocol.CampaignState{})
	if !messageFor("", local) || messageFor("dana", local) {
		t.Fatal("local snapshot scoping broken")
	}
}
