package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

// Hashed build assets may be cached forever; everything else revalidates.
// Text assets gzip when the browser accepts it.
func TestStaticCacheHeadersAndGzip(t *testing.T) {
	_, ts, _ := testServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	if _, err := client.Get(ts.URL + "/?token=testtoken"); err != nil {
		t.Fatal(err)
	}

	get := func(path string, acceptGzip bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest("GET", ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if acceptGzip {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	// index: revalidates, gzips, and announces the vary
	resp := get("/", true)
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("index cache-control %q, want no-cache", cc)
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("index content-encoding %q, want gzip", ce)
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("index vary %q, want Accept-Encoding", vary)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("index body is not gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(body)), "<html") {
		t.Fatal("gunzipped index is not html")
	}

	// a client that does not accept gzip gets the plain body
	plain := get("/", false)
	if ce := plain.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("plain request came back %q-encoded", ce)
	}

	// hashed assets are immutable and compressible
	matches, err := fs.Glob(distFileSystem(), "assets/*.js")
	if err != nil || len(matches) == 0 {
		t.Fatalf("no built js in the embedded dist: %v %v", matches, err)
	}
	asset := get("/"+matches[0], true)
	if cc := asset.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control %q, want immutable", cc)
	}
	if ce := asset.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("asset content-encoding %q, want gzip", ce)
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

// bootFake answers the engine's full bootstrap; handlers are swappable at
// runtime and see the call context, so a test can park a call until its
// context dies.
type bootFake struct {
	mu       sync.Mutex
	handlers map[gomsf.MsfRpcMethod]func(ctx context.Context, args ...interface{}) (interface{}, error)
}

func (b *bootFake) Call(ctx context.Context, method gomsf.MsfRpcMethod, args ...interface{}) (interface{}, error) {
	b.mu.Lock()
	h := b.handlers[method]
	b.mu.Unlock()
	if h == nil {
		return nil, fmt.Errorf("bootFake: no handler for %s", method)
	}
	return h(ctx, args...)
}

func (b *bootFake) set(method gomsf.MsfRpcMethod, h func(ctx context.Context, args ...interface{}) (interface{}, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[method] = h
}

func newBootFake() *bootFake {
	b := &bootFake{handlers: make(map[gomsf.MsfRpcMethod]func(ctx context.Context, args ...interface{}) (interface{}, error))}
	okList := func(key string) func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return func(ctx context.Context, args ...interface{}) (interface{}, error) {
			return map[string]interface{}{key: []interface{}{}}, nil
		}
	}
	setList := func(method gomsf.MsfRpcMethod, key string) {
		b.set(method, func(ctx context.Context, args ...interface{}) (interface{}, error) {
			return map[string]interface{}{key: []interface{}{}}, nil
		})
	}
	b.set(gomsf.AuthLogin, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"result": "success", "token": "tok"}, nil
	})
	b.set(gomsf.CoreVersion, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"version": "6.5.2", "ruby": "3.3", "api": "1.0"}, nil
	})
	b.set(gomsf.SessionList, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	b.set(gomsf.JobList, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{}, nil
	})
	b.set(gomsf.DbCurrentWorkspace, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"workspace": "default"}, nil
	})
	setList(gomsf.ModuleExploits, "modules")
	setList(gomsf.ModuleAuxiliary, "modules")
	setList(gomsf.ModulePost, "modules")
	setList(gomsf.ModulePayloads, "modules")
	setList(gomsf.ModuleEncoders, "modules")
	setList(gomsf.ModuleNops, "modules")
	setList(gomsf.ModuleEvasion, "modules")
	b.set(gomsf.DbHosts, okList("hosts"))
	b.set(gomsf.DbServices, okList("services"))
	b.set(gomsf.DbCreds, okList("creds"))
	b.set(gomsf.DbLoots, okList("loots"))
	b.set(gomsf.DbWorkspaces, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"workspaces": []interface{}{"default"}}, nil
	})
	var consoles int32
	b.set(gomsf.ConsoleCreate, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		id := atomic.AddInt32(&consoles, 1)
		return map[string]interface{}{"id": fmt.Sprintf("%d", id-1)}, nil
	})
	b.set(gomsf.ConsoleWrite, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"wrote": true}, nil
	})
	b.set(gomsf.ConsoleRead, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})
	return b
}

// Eight commands stuck in the engine while the tcp link dies: the reader is
// parked on the command semaphore and the writer is idle, so only the
// keepalive notices - its failure must close the connection and cancel the
// stuck commands' contexts.
func TestKeepaliveFailureCancelsStuckCommands(t *testing.T) {
	fake := newBootFake()
	e := engine.New(engine.Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if eb := e.Connect(context.Background(), protocol.ConnectParams{}); eb != nil {
		t.Fatalf("connect: %+v", eb)
	}
	s := New(e, "testtoken", Options{Version: "test", Keepalive: 20 * time.Millisecond})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	c := dialWS(t, wsURL, "testtoken")
	readMsg(t, c) // hello
	readMsg(t, c) // snapshot

	// every workspace.list parks until its command context dies
	entered := make(chan struct{}, maxConcurrentCommands+1)
	var exited atomic.Int32
	fake.set(gomsf.DbWorkspaces, func(ctx context.Context, args ...interface{}) (interface{}, error) {
		entered <- struct{}{}
		<-ctx.Done()
		exited.Add(1)
		return nil, fmt.Errorf("%w: blocked", gomsf.ErrRPC)
	})
	send := func(id int64) {
		if err := c.WriteJSON(protocol.ClientMessage{
			Type: protocol.KindCommand, ID: id, Method: protocol.MethodWorkspaceList,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := int64(1); i <= maxConcurrentCommands; i++ {
		send(i)
	}
	// the ninth parks the reader on the semaphore; nothing else is left to
	// notice the dead link
	send(maxConcurrentCommands + 1)
	deadline := time.After(5 * time.Second)
	for i := 0; i < maxConcurrentCommands; i++ {
		select {
		case <-entered:
		case <-deadline:
			t.Fatalf("only %d of %d commands reached the rpc layer", i, maxConcurrentCommands)
		}
	}
	time.Sleep(100 * time.Millisecond) // let the reader park on the ninth send

	c.UnderlyingConn().Close() // kill the tcp link; no close handshake

	deadline2 := time.After(5 * time.Second)
	for {
		if int(exited.Load()) == maxConcurrentCommands {
			return // the dead link released every stuck command
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline2:
			t.Fatalf("stuck commands were never cancelled when the link died: %d of %d released",
				exited.Load(), maxConcurrentCommands)
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
