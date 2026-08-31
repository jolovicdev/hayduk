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
	"runtime"
	"syscall"

	"github.com/jolovicdev/hayduk/internal/engine"
	"github.com/jolovicdev/hayduk/internal/server"
)

var version = "0.1.1"

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "host:port to bind")
	team := flag.Bool("team", false, "shared campaign for several operators on a trusted network")
	dev := flag.Bool("dev", false, "proxy UI to a vite dev server")
	devUpstream := flag.String("dev-upstream", "http://127.0.0.1:5173", "vite dev server URL")
	noBrowser := flag.Bool("no-browser", false, "do not open a browser")
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
			fmt.Fprintln(os.Stderr, "hayduk: --team needs an explicit --listen bind, e.g. --listen 0.0.0.0:8787")
			os.Exit(1)
		}
		if host, _, err := net.SplitHostPort(*listen); err == nil && loopbackOnly(host) {
			fmt.Fprintln(os.Stderr, "hayduk: --team needs a non-loopback --listen bind; operators connect from other machines")
			os.Exit(1)
		}
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		fmt.Fprintln(os.Stderr, "hayduk: cannot generate token:", err)
		os.Exit(1)
	}
	token := hex.EncodeToString(tokenBytes)

	eng := engine.New(engine.Config{})
	defer eng.Shutdown()

	opts := server.Options{Version: version, Team: *team}
	if *dev {
		opts.DevUpstream = *devUpstream
	}
	srv := server.New(eng, token, opts)

	url, err := srv.Listen(*listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hayduk: listen:", err)
		os.Exit(1)
	}

	fmt.Println("hayduk", version, "-", url)
	fmt.Println(url + "/?token=" + token)
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
