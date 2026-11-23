package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/server"
	"github.com/jolovicdev/hayduk/internal/store"
)

var version = "1.0.0"

// operatorFlags collects repeatable --operator name=token assignments.
type operatorFlags []string

func (o *operatorFlags) String() string { return strings.Join(*o, ",") }

func (o *operatorFlags) Set(value string) error {
	*o = append(*o, value)
	return nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "host:port to bind")
	team := flag.Bool("team", false, "shared campaign for several operators on a trusted network")
	dev := flag.Bool("dev", false, "proxy UI to a vite dev server")
	devUpstream := flag.String("dev-upstream", "http://127.0.0.1:5173", "vite dev server URL")
	noBrowser := flag.Bool("no-browser", false, "do not open a browser")
	noPersist := flag.Bool("no-persist", false, "do not write the campaign audit log or saved state to disk")
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for the campaign audit log and saved state")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (PEM); serves the UI over HTTPS")
	tlsKey := flag.String("tls-key", "", "TLS private key file (PEM)")
	var operatorSpecs operatorFlags
	flag.Var(&operatorSpecs, "operator", "name=token binding a named operator to an access token (team mode; repeatable)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("hayduk", version)
		return
	}

	listenSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "listen" {
			listenSet = true
		}
	})
	if *team {
		if !listenSet {
			fmt.Fprintln(os.Stderr, "hayduk: --team needs an explicit --listen bind, e.g. --listen 192.168.1.10:8787")
			os.Exit(1)
		}
		if host, _, err := net.SplitHostPort(*listen); err == nil {
			if wildcardHost(host) {
				// a wildcard cannot be advertised: remote operators would be
				// handed an unusable http://[::]:port link
				fmt.Fprintln(os.Stderr, "hayduk: --team needs a specific interface address, not a wildcard bind; find one with `ip addr`, e.g. --listen 192.168.1.10:8787")
				os.Exit(1)
			}
			if loopbackOnly(host) {
				fmt.Fprintln(os.Stderr, "hayduk: --team needs a non-loopback --listen bind; operators connect from other machines")
				os.Exit(1)
			}
		}
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		fmt.Fprintln(os.Stderr, "hayduk: --tls-cert and --tls-key must be given together")
		os.Exit(1)
	}

	operators, err := parseOperatorTokens(operatorSpecs, *team)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hayduk:", err)
		os.Exit(1)
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		fmt.Fprintln(os.Stderr, "hayduk: cannot generate token:", err)
		os.Exit(1)
	}
	token := hex.EncodeToString(tokenBytes)

	// Persistence is best effort: an unwritable directory must not stop an
	// operator from working, it only costs the history.
	var st *store.Store
	if !*noPersist && *dataDir != "" {
		opened, err := store.Open(*dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hayduk: state disabled:", err)
		} else {
			st = opened
			defer st.Close() // runs after eng.Shutdown (LIFO)
		}
	}
	eng := engine.New(engine.Config{Store: st})
	defer eng.Shutdown()

	opts := server.Options{Version: version, Team: *team, Operators: operators}
	if *dev {
		opts.DevUpstream = *devUpstream
	}
	srv := server.New(eng, token, opts)

	var url string
	if *tlsCert != "" {
		url, err = srv.ListenTLS(*listen, *tlsCert, *tlsKey)
	} else {
		url, err = srv.Listen(*listen)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hayduk: listen:", err)
		os.Exit(1)
	}

	fmt.Println("hayduk", version, "-", url)
	fmt.Println(url + "/?token=" + token)
	for _, spec := range operatorSpecs {
		name, tok, _ := strings.Cut(spec, "=")
		fmt.Println("operator "+strings.TrimSpace(name)+":", url+"/?token="+strings.TrimSpace(tok))
	}
	if st != nil {
		fmt.Println("state directory:", st.Dir())
	}
	if *team {
		fmt.Println("team mode: share that link with operators on a trusted network; it carries the auth token")
	}

	if !*noBrowser && !*team {
		openBrowser(url + "/?token=" + token)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	srv.Close()
}

// wildcardHost reports whether a --listen host binds every interface: the
// empty host (":port"), 0.0.0.0, or ::. Hostnames are left alone - they name
// one machine, and net.Listen reports anything unresolvable itself.
func wildcardHost(host string) bool {
	if host == "" {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().IsUnspecified() // "::ffff:0.0.0.0" is the mapped unspecified
	}
	return false
}

// loopbackOnly reports whether a --listen host can serve nothing but this
// machine: a literal loopback IP (scoped IPv6 included, which netip parses
// and Go can bind but net.ParseIP rejects), or a hostname resolving
// exclusively to loopback (localhost never parses as an IP). Unresolvable
// names are left to net.Listen, which fails with its own error.
func loopbackOnly(host string) bool {
	if host == "" {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback()
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ip, err := netip.ParseAddr(a); err != nil || !ip.IsLoopback() {
			return false
		}
	}
	return len(addrs) > 0
}

// parseOperatorTokens validates the repeatable --operator flags. The tokens
// are access secrets, so they must be long enough to resist guessing, unique,
// and each name may appear once.
func parseOperatorTokens(specs []string, team bool) (map[string]string, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if !team {
		return nil, fmt.Errorf("--operator needs --team: per-operator tokens exist to share one campaign")
	}
	operators := make(map[string]string, len(specs))
	names := make(map[string]bool, len(specs))
	for _, spec := range specs {
		name, token, ok := strings.Cut(spec, "=")
		name, token = strings.TrimSpace(name), strings.TrimSpace(token)
		if !ok || name == "" || token == "" {
			return nil, fmt.Errorf("--operator wants name=token, got %q", spec)
		}
		if len(token) < 16 {
			return nil, fmt.Errorf("operator %q has a token shorter than 16 characters", name)
		}
		if names[name] {
			return nil, fmt.Errorf("operator %q is given more than once", name)
		}
		if _, exists := operators[token]; exists {
			return nil, fmt.Errorf("operator %q reuses another operator's token", name)
		}
		operators[token] = name
		names[name] = true
	}
	return operators, nil
}

// defaultDataDir is the per-user directory for the audit log and saved
// campaign state. An empty result means this platform has no config
// directory, in which case persistence stays off unless --data-dir is given.
func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "hayduk")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}
