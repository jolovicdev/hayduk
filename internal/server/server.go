// Package server hosts the UI and the operator WebSocket on localhost.
package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

const cookieName = "hayduk"

type Options struct {
	Version string
	// Team marks shared-campaign mode: the browser asks for operator names
	// and commands carry attribution.
	Team bool
	// DevUpstream, when set, proxies all non-API requests to a vite dev
	// server instead of the embedded dist.
	DevUpstream string
	// Keepalive is the websocket ping interval; tests shrink it.
	Keepalive time.Duration
}

func (o Options) keepalive() time.Duration {
	if o.Keepalive <= 0 {
		return 30 * time.Second
	}
	return o.Keepalive
}

// maxConcurrentCommands bounds in-flight command goroutines per socket so a
// runaway client cannot spawn unbounded work against msfrpcd.
const maxConcurrentCommands = 8

type Server struct {
	engine engineInterface
	token  string
	opts   Options
	ln     net.Listener
}

// engineInterface is the engine surface the server needs; *engine.Engine
// satisfies it. Declared here so server tests can run without msfrpcd.
type engineInterface interface {
	State() protocol.CampaignState
	SubscribeSnapshot() (*engine.Subscription, protocol.CampaignState)
	Exec(ctx context.Context, operator, method string, params json.RawMessage) (json.RawMessage, *protocol.ErrorBody)
	OperatorJoin(name string)
	OperatorLeave(name string)
}

func New(e *engine.Engine, token string, opts Options) *Server {
	return &Server{engine: e, token: token, opts: opts}
}

func (s *Server) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	s.ln = ln
	go http.Serve(ln, s.Handler())
	return "http://" + ln.Addr().String(), nil
}

func (s *Server) Close() {
	if s.ln != nil {
		s.ln.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/", s.guard(http.HandlerFunc(s.handleStatic)))
	return mux
}

// guard enforces the one-time token: first hit with ?token= sets the cookie
// and redirects to the bare path; later hits need the cookie.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}
		if token := r.URL.Query().Get("token"); token != "" && token == s.token {
			http.SetCookie(w, &http.Cookie{
				Name: cookieName, Value: s.token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.Error(w, "hayduk: invalid or missing token", http.StatusForbidden)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	ck, err := r.Cookie(cookieName)
	return err == nil && ck.Value == s.token
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.opts.DevUpstream != "" {
		s.devProxy(w, r)
		return
	}
	// Hashed build assets are immutable: a browser may keep them forever.
	// Everything else - index.html, favicon - revalidates so a new binary's
	// UI is picked up on the next load.
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Method == http.MethodGet &&
		strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") &&
		gzipCompressible(r.URL.Path) {
		gz := &gzipResponseWriter{ResponseWriter: w, gw: gzip.NewWriter(w)}
		defer gz.Close()
		http.FileServerFS(distFileSystem()).ServeHTTP(gz, r)
		return
	}
	http.FileServerFS(distFileSystem()).ServeHTTP(w, r)
}

// gzipCompressible reports whether the static file at p is worth a gzip pass;
// the woff2 fonts are deflate-compressed already and gain nothing from another.
func gzipCompressible(p string) bool {
	if p == "/" {
		return true // serves index.html
	}
	switch path.Ext(p) {
	case ".html", ".js", ".mjs", ".css", ".svg", ".json", ".txt", ".webmanifest", ".map":
		return true
	}
	return false
}

// gzipResponseWriter streams the file server's reply through gzip. The
// compressed length is unknown up front, so ServeContent's Content-Length is
// dropped and Go falls back to chunked transfer. Bodyless statuses (304, 204)
// pass through untouched - they must not grow a gzip trailer.
type gzipResponseWriter struct {
	http.ResponseWriter
	gw        *gzip.Writer
	wroteGzip bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if code != http.StatusNotModified && code != http.StatusNoContent {
		h := g.Header()
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		g.wroteGzip = true
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteGzip {
		return g.ResponseWriter.Write(b) // headers already went out plain
	}
	return g.gw.Write(b)
}

func (g *gzipResponseWriter) Close() {
	if g.wroteGzip {
		g.gw.Close() // flushes the gzip trailer
	}
}

func (s *Server) devProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(s.opts.DevUpstream)
	if err != nil {
		http.Error(w, "bad dev upstream", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // stream hmr updates
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "dev server unreachable: "+err.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	},
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	go s.serveConn(conn)
}

type wsConn struct {
	conn   *websocket.Conn
	out    chan any
	done   chan struct{} // closed with out; ends the pump without waiting for the bus
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func (c *wsConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *wsConn) closeLocked() {
	if c.closed {
		return
	}
	c.closed = true
	close(c.out)
	close(c.done)
	if c.cancel != nil {
		c.cancel() // command contexts die with the socket
	}
}

// send is the only writer feeder. Holding mu across the channel send makes
// close and send mutually exclusive, so nothing can send on a closed channel.
func (c *wsConn) send(m any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.out <- m:
	default:
		// writer stalled; drop the connection (client resnapshots)
		c.closeLocked()
	}
}

func (c *wsConn) ping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	return c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) == nil
}

func (s *Server) serveConn(conn *websocket.Conn) {
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	c := &wsConn{conn: conn, out: make(chan any, 256), done: make(chan struct{}), cancel: cmdCancel}

	// The subscription and the snapshot come out of one engine-lock hold:
	// broadcasts already folded into the snapshot are suppressed for this
	// socket by the bus, and everything sent after lands in order behind it.
	// Neither side of the handshake can starve the other - subscribing alone
	// would replay pre-snapshot messages (reverting newer state), snapshotting
	// alone would drop updates that land in between.
	sub, snap := s.engine.SubscribeSnapshot()
	// the pump exits on c.done when the connection dies, so the subscription
	// is released here rather than after the wait
	defer sub.Stop()

	c.send(protocol.NewHello(s.opts.Version, s.opts.Team))
	c.send(protocol.NewSnapshot(snap))

	// writer: single goroutine owns conn writes; a write that stalls longer
	// than the deadline kills the connection instead of freezing every
	// downstream broadcast
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for m := range c.out {
			conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := conn.WriteJSON(m); err != nil {
				c.close()
				return
			}
		}
	}()

	// pump engine broadcasts into out
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.close()
		for {
			select {
			case m, ok := <-sub.C():
				if !ok {
					return // bus dropped us; the client resnapshots
				}
				c.send(m)
			case <-c.done:
				return
			}
		}
	}()

	// reader
	go func() {
		defer c.close()
		operator := ""
		defer func() {
			if operator != "" {
				s.engine.OperatorLeave(operator)
			}
		}()
		conn.SetReadLimit(1 << 20)
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			return nil
		})
		sem := make(chan struct{}, maxConcurrentCommands)
		for {
			var msg protocol.ClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			if msg.Type != protocol.KindCommand {
				continue
			}
			if msg.Operator != "" && msg.Operator != operator {
				// a renamed operator (team mode identity switch) moves
				// presence and attribution; only the current name lingers
				if operator != "" {
					s.engine.OperatorLeave(operator)
				}
				operator = msg.Operator
				s.engine.OperatorJoin(operator)
			}
			op := operator
			select {
			case sem <- struct{}{}:
			case <-cmdCtx.Done():
				return
			}
			go func(m protocol.ClientMessage) {
				defer func() { <-sem }()
				data, errBody := s.engine.Exec(cmdCtx, op, m.Method, m.Params)
				if errBody != nil {
					c.send(protocol.ErrorResponse(m.ID, errBody.Code, errBody.Message))
					return
				}
				c.send(protocol.OKResponse(m.ID, json.RawMessage(data)))
			}(msg)
		}
	}()

	// keepalive
	go func() {
		ticker := time.NewTicker(s.opts.keepalive())
		defer ticker.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-ticker.C:
				if !c.ping() {
					// a dead link must close the connection, not just stop
					// the keepalive: closing also cancels the command
					// contexts, the only thing that frees a parked reader
					c.close()
					return
				}
			}
		}
	}()

	c.wg.Wait()
	sub.Stop()
	cmdCancel()
	conn.Close()
}
