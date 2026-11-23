// Package server hosts the UI and the operator WebSocket on localhost.
package server

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

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
	// Operators maps an access token to the operator name it binds. The
	// primary token stays anonymous (the browser picks its label); the map
	// is empty in single-operator and shared-token team mode.
	Operators map[string]string
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
	// cookieName is derived from the token: browser cookies are not
	// partitioned by port, so two instances on one host with the shared
	// name "hayduk" would keep overwriting each other's authentication.
	cookieName string
	// identityByToken binds per-operator tokens to names. The primary token
	// is not in it: it leaves the operator label to the browser.
	identityByToken map[string]string
	secure          bool
	opts            Options
	ln              net.Listener
}

// engineInterface is the engine surface the server needs; *engine.Engine
// satisfies it. Declared here so server tests can run without msfrpcd.
type engineInterface interface {
	State() protocol.CampaignState
	SubscribeSnapshot() (*engine.Subscription, protocol.CampaignState)
	SubscribeSnapshotFor(operator string) (*engine.Subscription, protocol.CampaignState)
	ConsoleFor(operator string) *protocol.ConsoleState
	InteractFor(operator string) *protocol.InteractState
	Exec(ctx context.Context, operator, method string, params json.RawMessage) (json.RawMessage, *protocol.ErrorBody)
	OperatorJoin(name string)
	OperatorLeave(name string)
}

func New(e *engine.Engine, token string, opts Options) *Server {
	sum := sha256.Sum256([]byte(token))
	identities := make(map[string]string, len(opts.Operators))
	for tok, name := range opts.Operators {
		if tok != "" && name != "" {
			identities[tok] = name
		}
	}
	return &Server{
		engine: e, token: token, opts: opts,
		cookieName:      "hayduk-" + hex.EncodeToString(sum[:6]),
		identityByToken: identities,
	}
}

func (s *Server) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	s.ln = ln
	go http.Serve(ln, s.Handler())
	return "http://" + advertiseAddr(ln.Addr().String()), nil
}

// ListenTLS serves the UI over TLS with the given certificate and key. The
// cookie becomes Secure; the advertised URL uses https (and therefore wss for
// the socket). The key pair is loaded before binding so a bad certificate
// fails at startup, not on the first browser.
func (s *Server) ListenTLS(addr, certFile, keyFile string) (string, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	s.ln = tlsLn
	s.secure = true
	go http.Serve(tlsLn, s.Handler())
	return "https://" + advertiseAddr(ln.Addr().String()), nil
}

// advertiseAddr rewrites an unspecified bind (0.0.0.0, [::]) into the
// loopback address a browser can actually open: a wildcard is not a
// destination, and printing one hands the operator a dead URL. A specific
// address is advertised verbatim. (An IPv6 wildcard listener is dual-stack,
// so the IPv4 loopback reaches it.)
func advertiseAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Unmap().IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
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
	return s.securityHeaders(mux)
}

// securityHeaders hardens every response: no framing, no MIME sniffing, no
// referrer leakage, and a CSP that only admits this origin (plus the dev
// server's inline transforms when proxying to Vite).
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	script := "'self'"
	if s.opts.DevUpstream != "" {
		script = "'self' 'unsafe-inline' 'unsafe-eval'"
	}
	csp := "default-src 'self'; " +
		"script-src " + script + "; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self'; " +
		"connect-src 'self' ws: wss:; " +
		"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// guard enforces the one-time token: first hit with ?token= sets the cookie
// and redirects to the bare path; later hits need the cookie.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizedToken(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		if token := r.URL.Query().Get("token"); token != "" {
			if _, ok := s.tokenIdentity(token); ok {
				http.SetCookie(w, &http.Cookie{
					Name: s.cookieName, Value: token, Path: "/",
					HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}
		http.Error(w, "hayduk: invalid or missing token", http.StatusForbidden)
	})
}

// tokenIdentity resolves an access token. The boolean says the token is
// valid; the name is empty for the shared primary token, whose operator
// label the browser chooses.
func (s *Server) tokenIdentity(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	if token == s.token {
		return "", true
	}
	name, ok := s.identityByToken[token]
	return name, ok
}

func (s *Server) authorizedToken(r *http.Request) (string, bool) {
	ck, err := r.Cookie(s.cookieName)
	if err != nil {
		return "", false
	}
	if _, ok := s.tokenIdentity(ck.Value); !ok {
		return "", false
	}
	return ck.Value, true
}

func (s *Server) authorized(r *http.Request) bool {
	_, ok := s.authorizedToken(r)
	return ok
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
	token, ok := s.authorizedToken(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// A per-operator token fixes the identity for this socket; the shared
	// token leaves the name empty and the browser supplies it.
	bound, _ := s.tokenIdentity(token)
	go s.serveConn(conn, bound)
}

type wsConn struct {
	conn   *websocket.Conn
	out    chan any
	done   chan struct{} // closed with out; ends the pump without waiting for the bus
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	// operator is the name this socket joined under; the pump uses it to
	// keep personal resources (console, interact, snapshots) scoped to
	// this socket's operator. Guarded by mu.
	operator string
	wg       sync.WaitGroup
}

func (c *wsConn) setOperator(name string) {
	c.mu.Lock()
	c.operator = name
	c.mu.Unlock()
}

func (c *wsConn) getOperator() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.operator
}

// messageFor reports whether a broadcast belongs to this socket's operator.
// Shared resources (events, sessions, hosts, ...) pass for everyone; the
// personal ones - console and interact updates, console output, snapshots -
// carry the operator they describe and reach only that operator's sockets.
func messageFor(operator string, m protocol.ServerMessage) bool {
	switch v := m.(type) {
	case protocol.Snapshot:
		return v.Operator == operator
	case protocol.ConsoleOutputMsg:
		return v.Operator == operator
	case protocol.ResourceUpdate:
		if v.Resource == protocol.ResConsole || v.Resource == protocol.ResInteract {
			return v.Operator == operator
		}
	}
	return true
}

// operatorNameLimit bounds a joined name; names are labels on a trusted
// network, not identities, but they render in every operator's event log.
const operatorNameLimit = 32

func validOperatorName(name string) bool {
	if name == "" || len([]rune(name)) > operatorNameLimit {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
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

func (s *Server) serveConn(conn *websocket.Conn, bound string) {
	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	c := &wsConn{conn: conn, out: make(chan any, 256), done: make(chan struct{}), cancel: cmdCancel}

	hello := protocol.NewHello(s.opts.Version, s.opts.Team)
	hello.Identity = bound
	c.send(hello)

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

	// pump engine broadcasts into out, keeping only what belongs to this
	// socket's operator: shared resources reach everyone, personal ones
	// (console, interact, snapshots) reach their owner alone.
	startPump := func(sub *engine.Subscription) {
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
					if messageFor(c.getOperator(), m) {
						c.send(m)
					}
				case <-c.done:
					return
				}
			}
		}()
	}

	// reader
	go func() {
		defer c.close()
		operator := ""
		var sub *engine.Subscription
		defer func() {
			if sub != nil {
				sub.Stop()
			}
			if operator != "" {
				s.engine.OperatorLeave(operator)
			}
		}()

		// The subscription and the snapshot come out of one engine-lock
		// hold: broadcasts already folded into the snapshot are suppressed
		// for this socket by the bus, and everything sent after lands in
		// order behind it. Neither side of the handshake can starve the
		// other - subscribing alone would replay pre-snapshot messages
		// (reverting newer state), snapshotting alone would drop updates
		// that land in between. Single mode subscribes at once under the
		// local "" operator; team mode on the first join, so the snapshot
		// carries the joined operator's console and interact state.
		subscribe := func(name string) {
			var snap protocol.CampaignState
			sub, snap = s.engine.SubscribeSnapshotFor(name)
			c.send(protocol.NewSnapshotFor(name, snap))
			startPump(sub)
		}
		// register moves presence to a name and hands the socket that
		// name's personal state: a full snapshot on the first join, the
		// console and interact resources on a rename.
		register := func(name string) {
			if name == operator {
				return
			}
			if operator != "" {
				s.engine.OperatorLeave(operator)
			}
			operator = name
			c.setOperator(name)
			s.engine.OperatorJoin(name)
			if sub == nil {
				subscribe(name)
			} else {
				c.send(protocol.ConsoleUpdate(name, s.engine.ConsoleFor(name)))
				c.send(protocol.InteractUpdate(name, s.engine.InteractFor(name)))
			}
		}

		if !s.opts.Team {
			subscribe("")
		} else if bound != "" {
			// A bound per-operator token registers and snapshots immediately
			// under its fixed identity.
			register(bound)
		}

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
			name := strings.TrimSpace(msg.Operator)
			if msg.Method == protocol.MethodOperatorJoin {
				// explicit presence registration; a join or rename must
				// not wait for the next command to carry the name. A bound
				// token's identity is fixed by the server.
				if bound != "" {
					c.send(protocol.OKResponse(msg.ID, nil))
					continue
				}
				if !validOperatorName(name) {
					c.send(protocol.ErrorResponse(msg.ID, protocol.CodeBadParams,
						"operator name must be 1-32 printable characters"))
					continue
				}
				register(name)
				c.send(protocol.OKResponse(msg.ID, nil))
				continue
			}
			if bound == "" {
				if s.opts.Team && operator == "" {
					// team commands need an identity: attribution and the
					// personal console both ride on the joined name
					if !validOperatorName(name) {
						c.send(protocol.ErrorResponse(msg.ID, protocol.CodeBadParams,
							"choose an operator name first"))
						continue
					}
					register(name)
				} else if name != "" && name != operator && validOperatorName(name) {
					// a renamed operator (team mode identity switch) moves
					// presence and attribution; only the current name lingers
					register(name)
				}
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
	cmdCancel()
	conn.Close()
}
