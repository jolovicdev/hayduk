# Hayduk: the open-source Metasploit GUI and Armitage alternative

[![CI](https://github.com/jolovicdev/hayduk/actions/workflows/ci.yml/badge.svg)](https://github.com/jolovicdev/hayduk/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jolovicdev/hayduk?display_name=tag)](https://github.com/jolovicdev/hayduk/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-informational)](LICENSE)
[![Go 1.26.1+](https://img.shields.io/badge/go-1.26.1%2B-00ADD8)](https://go.dev)

Hayduk is a free, open-source **Metasploit GUI** built as a modern
Armitage alternative. This graphical attack management console is a
single Go binary with a browser UI. Hayduk connects to
[msfrpcd](https://docs.rapid7.com/metasploit/rpc-api/) through
[go-msf](https://github.com/jolovicdev/go-msf) and uses the modules,
payloads and Meterpreter sessions from your existing Metasploit installation.

**For authorized security testing only.**

![Hayduk demo: launch a module from the tree, attack the detected port, land a live shell session](docs/demo.gif)

## What you get

- **Live network topology**: the graph Armitage made famous, with hosts
  grouped by subnet, access states, pivot routes drawn as dashed edges,
  node positions that survive reloads
- **Campaign workflows**: host discovery and service scans, login attacks
  with pre-filled recovered credentials, and Find attacks matching exploits
  to a host's services
- **Hail Mary**: the Armitage signature move launches every matching exploit
  against the selected hosts; launches are paced and recorded in the event log
- **Sessions**: interact with Meterpreter and shell sessions, upgrade shells
  to Meterpreter or terminate sessions; output streams live with the real
  prompt and busy state
- **Module launcher**: the full module tree with reliability ranks, option
  editing, payload selection
- **Credentials, loot and events**: everything the workspace database
  knows, plus an attributed event log
- **Report export**: one self-contained HTML document summarizing the
  campaign for review and client delivery
- **Team mode**: several operators on one shared campaign (trusted
  networks)

## Quickstart

Download a prebuilt binary for Linux, macOS or Windows on amd64 or arm64
from the
[releases page](https://github.com/jolovicdev/hayduk/releases/latest).
Each platform archive contains one static Hayduk binary. Hayduk itself
needs no JVM, installer or runtime libraries. It connects to a separate
Metasploit instance.

For Linux on amd64:

    tar xf hayduk_*_linux_amd64.tar.gz && ./hayduk

Prefer to build from source? You need Go 1.26.1+ and Node:

    make            # builds the UI, embeds it, compiles bin/hayduk
    ./bin/hayduk

Either way, the console opens in your browser. Point it at a running
`msfrpcd`. On Kali Linux, install Metasploit Framework first if needed:

    sudo apt install metasploit-framework

Then start its RPC daemon:

    msfrpcd -P yourpassword -S -f -a 127.0.0.1

or spin the disposable docker lab used by the integration tests:

    scripts/msf/up.sh                  # msfrpcd on 127.0.0.1:55553, user msf / testpass123
    scripts/msf/up.sh --with-vulnbox   # plus disposable target boxes to scan and attack
    scripts/msf/down.sh

The demo above was recorded against exactly that lab — one command, no
targets of your own needed.

## Why Hayduk

Armitage established the graphical attack management workflow for
Metasploit, but its
[latest commit](https://github.com/rsmudge/armitage/commit/c8ca6c00b5584444ef3c3a8e32341f43974567bd)
was in July 2016. Rapid7
[removed msfgui and Armitage](https://github.com/rapid7/metasploit-framework/commit/1112daaff2d493a51ab7fb4e8c823e21a1ac9edd)
from the Metasploit Framework distribution in April 2013 and
[ended availability of Metasploit Community Edition](https://www.rapid7.com/blog/post/2019/07/18/end-of-sale-announced-for-metasploit-community/)
in July 2019.

Hayduk rebuilds that workflow around the Metasploit RPC API. It keeps the
live graph, Hail Mary and shared campaigns while replacing the Java desktop
client with a browser and a single Go binary.

## FAQ

### Is Hayduk an Armitage replacement?

Hayduk follows the same attack management model, including the network
graph, Hail Mary and shared campaigns. It is a separate implementation
that drives `msfrpcd` directly and uses a browser instead of a Java client.

### Does Hayduk include Metasploit?

No. Hayduk is an `msfrpcd` client for the Metasploit Framework you already
run, including Kali's `metasploit-framework` package. Install Metasploit,
start `msfrpcd`, then point Hayduk at it.

### Which platforms does Hayduk run on?

Prebuilt Hayduk binaries support Linux, macOS and Windows on amd64 and
arm64. Hayduk can connect to `msfrpcd` running on another machine.

## Team mode

    ./bin/hayduk --team --listen 192.168.1.10:8787

Team mode is Hayduk's team server. It requires an explicit bind on a
specific, non-loopback interface address — the one from `ip addr` that
operators can reach; wildcard binds are refused because the printed link
must carry a usable host. Every operator opens the printed token link, picks
a name, and that name rides on their commands and lands next to their
actions in the shared event log. Authentication uses the token URL. Treat
the link like a password and only run team mode on networks you trust.

## Testing

    make test         # go test ./... + ui tests
    make integration  # against the docker stack above

## Development

Terminal 1: cd ui && npm run dev
Terminal 2: make dev
Open the URL that Hayduk prints. The UI hot-reloads through the dev proxy.

Protocol types are generated: `make gen` after touching
`internal/protocol/protocol.go`; `make gen-check` catches drift.

## Credits

Design lineage: [Armitage](https://github.com/rsmudge/armitage) by
Raphael Mudge.

License: MIT
