package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func testServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	e := engine.New(engine.Config{})
	t.Cleanup(e.Shutdown)
	s := New(e, "testtoken", Options{Version: "test"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	return s, ts, wsURL
}

func dialWS(t *testing.T, url, cookie string) *websocket.Conn {
	t.Helper()
	h := map[string][]string{"Origin": {strings.Replace(strings.Replace(url, "ws", "http", 1), "/ws", "", 1)}}
	if cookie != "" {
		h["Cookie"] = []string{"hayduk=" + cookie}
	}
	c, _, err := websocket.DefaultDialer.Dial(url, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func readMsg(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	var m map[string]any
	if err := c.ReadJSON(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWSRequiresToken(t *testing.T) {
	_, _, wsURL := testServer(t)
	if _, _, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil {
		t.Fatal("ws accepted without token")
	}
}

func TestWSHelloThenSnapshot(t *testing.T) {
	_, _, wsURL := testServer(t)
	c := dialWS(t, wsURL, "testtoken")

	hello := readMsg(t, c)
	if hello["type"] != "hello" || hello["version"] != "test" {
		t.Fatalf("hello %+v", hello)
	}
	snap := readMsg(t, c)
	if snap["type"] != "snapshot" {
		t.Fatalf("snapshot %+v", snap)
	}
	state, ok := snap["state"].(map[string]any)
	if !ok {
		t.Fatal("snapshot.state missing")
	}
	conn, ok := state["connection"].(map[string]any)
	if !ok || conn["status"] != "disconnected" {
		t.Fatalf("state.connection %+v", conn)
	}
}

func TestWSCommandResponse(t *testing.T) {
	_, _, wsURL := testServer(t)
	c := dialWS(t, wsURL, "testtoken")
	readMsg(t, c) // hello
	readMsg(t, c) // snapshot

	c.WriteJSON(protocol.ClientMessage{Type: protocol.KindCommand, ID: 42, Method: "bogus"})
	for {
		m := readMsg(t, c)
		if m["type"] != "response" {
			continue // ignore other broadcasts
		}
		if m["id"].(float64) != 42 {
			t.Fatalf("response id mismatch: %+v", m)
		}
		errBody := m["error"].(map[string]any)
		if errBody["code"] != protocol.CodeUnknownMethod {
			t.Fatalf("error %+v", errBody)
		}
		return
	}
}

func TestStaticServesUIWithToken(t *testing.T) {
	_, ts, _ := testServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	resp, err := client.Get(ts.URL + "/?token=testtoken")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d after token redirect", resp.StatusCode)
	}

	// plain path with the jar cookie must also pass
	resp2, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("status %d with cookie", resp2.StatusCode)
	}
}

func TestStaticRejectsWithoutToken(t *testing.T) {
	_, ts, _ := testServer(t)
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestWSCommandCancelledWhenSocketDies(t *testing.T) {
	inFlight := make(chan struct{}, 1)
	done := make(chan struct{})
	fake := &blockingRPC{onCall: func(ctx context.Context) {
		select {
		case inFlight <- struct{}{}:
		default:
		}
		<-ctx.Done()
		close(done)
	}}
	e := engine.New(engine.Config{RPC: fake})
	t.Cleanup(e.Shutdown)
	s := New(e, "testtoken", Options{Version: "test"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	c := dialWS(t, wsURL, "testtoken")
	readMsg(t, c) // hello
	readMsg(t, c) // snapshot

	if err := c.WriteJSON(protocol.ClientMessage{
		Type: protocol.KindCommand, ID: 1, Method: protocol.MethodConnect,
		Params: json.RawMessage(`{"host":"h","port":55553}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("command never reached the rpc layer")
	}
	c.Close() // socket dies mid-command; the command context must be cancelled
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command context survived socket closure")
	}
}

// blockingRPC routes every call through onCall and fails the RPC.
type blockingRPC struct {
	onCall func(ctx context.Context)
}

func (b *blockingRPC) Call(ctx context.Context, method gomsf.MsfRpcMethod, args ...interface{}) (interface{}, error) {
	b.onCall(ctx)
	return nil, fmt.Errorf("%w: blocked", gomsf.ErrRPC)
}

func TestOperatorRenameOverWS(t *testing.T) {
	s, _, wsURL := testServer(t)
	c := dialWS(t, wsURL, "testtoken")
	readMsg(t, c) // hello
	readMsg(t, c) // snapshot

	send := func(operator string) {
		if err := c.WriteJSON(protocol.ClientMessage{
			Type: protocol.KindCommand, ID: 9, Method: "db.refresh", Operator: operator,
		}); err != nil {
			t.Fatal(err)
		}
	}
	send("dana")
	waitForOperators := func(want string) {
		deadline := time.After(5 * time.Second)
		for {
			ops := s.engine.State().Operators
			if len(ops) == 1 && ops[0] == want {
				return
			}
			select {
			case <-time.After(10 * time.Millisecond):
			case <-deadline:
				t.Fatalf("operators never became [%s]: %+v", want, ops)
			}
		}
	}
	waitForOperators("dana")

	// renaming through the UI sends the new name on later commands; the
	// server must move presence and attribution instead of pinning the first
	send("mile")
	waitForOperators("mile")

	c.Close()
	deadline := time.After(5 * time.Second)
	for len(s.engine.State().Operators) != 0 {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("renamed operator never left: %+v", s.engine.State().Operators)
		}
	}
}

func TestKeepaliveGoroutineExitsAfterClose(t *testing.T) {
	_, _, wsURL := testServer(t)
	time.Sleep(50 * time.Millisecond) // let the server's own setup settle
	before := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		c := dialWS(t, wsURL, "testtoken")
		readMsg(t, c) // hello
		readMsg(t, c) // snapshot
		c.Close()
	}
	deadline := time.After(5 * time.Second)
	for {
		if runtime.NumGoroutine() <= before {
			return // every per-connection goroutine, keepalive included, exited
		}
		select {
		case <-deadline:
			t.Fatalf("per-connection goroutines lingered after close: %d -> %d", before, runtime.NumGoroutine())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestOperatorJoinOverWS(t *testing.T) {
	s, _, wsURL := testServer(t)
	c := dialWS(t, wsURL, "testtoken")
	readMsg(t, c) // hello
	readMsg(t, c) // snapshot

	if err := c.WriteJSON(protocol.ClientMessage{
		Type: protocol.KindCommand, ID: 9, Method: "db.refresh", Operator: "dana",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for len(s.engine.State().Operators) == 0 {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("operator never joined")
		}
	}
	if got := s.engine.State().Operators; len(got) != 1 || got[0] != "dana" {
		t.Fatalf("operators %+v", got)
	}

	c.Close()
	deadline = time.After(5 * time.Second)
	for len(s.engine.State().Operators) != 0 {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("operator never left after disconnect")
		}
	}
}
