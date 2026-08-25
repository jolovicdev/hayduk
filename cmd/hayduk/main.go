package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
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
		if host, _, err := net.SplitHostPort(*listen); err == nil {
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				fmt.Fprintln(os.Stderr, "hayduk: --team needs a non-loopback --listen bind; operators connect from other machines")
				os.Exit(1)
			}
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
