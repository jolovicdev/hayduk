# Hayduk — Open-Source Metasploit GUI & Armitage Alternative

[![CI](https://github.com/jolovicdev/hayduk/actions/workflows/ci.yml/badge.svg)](https://github.com/jolovicdev/hayduk/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jolovicdev/hayduk?display_name=tag)](https://github.com/jolovicdev/hayduk/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-informational)](LICENSE)

Hayduk is a free, open-source **Metasploit GUI** for authorized penetration testing. It brings the Armitage campaign workflow to your browser in a fast, zero-dependency Go binary: map hosts and OS hardware tiles, search 7,000+ modules with `Ctrl+K`, manage Meterpreter and shell sessions, pivot across internal subnets, coordinate in team mode, and export clean HTML reports.

**Download one binary. Run it. Connect to `msfrpcd`.** No Java, Node.js, or Go runtime required — the SolidJS browser UI is embedded in the executable.

Hayduk connects to a separate Metasploit Framework instance through `msfrpcd`. It does not bundle Metasploit.

**[Website (hayduk.dev)](https://hayduk.dev)** · **[Download latest release](https://github.com/jolovicdev/hayduk/releases/latest)** · [Quickstart](#quickstart) · [Try the Docker lab](#try-the-docker-lab) · [Team mode](#team-mode)

![Hayduk 1.0.0 Carbon Instrument workbench showing network topology with OS hardware icons, module tree, host inspector, and docked console](docs/screenshot.png)

*Hayduk 1.0.0 workbench shown with illustrative campaign data.*

---

## Capabilities

| Area | What you get |
|---|---|
| **Network topology** | Adaptive subnet zones with OS hardware tiles (Windows, Linux, macOS, Android, generic), live port counters, drag-and-pan canvas, Focus mode, and clickable routed-subnet cards. |
| **Command palette & module tree** | Instant `Ctrl+K` search across hosts, sessions, actions, and 7,000+ Metasploit modules, plus a debounced module tree with reliability ranks, required options, and compatible payloads. |
| **Engine-owned operations** | Host discovery, `db_nmap -sV` service fingerprinting, login brute force, and Hail Mary runs execute as tracked background operations in the **Operations** dock tab with live progress and cancellation. |
| **Multi-tab sessions** | Keep multiple Meterpreter and shell consoles attached in **Interact**, run structured Meterpreter tools (Sysinfo, Processes, Files, Screenshot, Keylogger, Hashdump), and upgrade shells. |
| **Pivot Studio** | Manage Metasploit routes, SOCKS proxies, and port forwards from one tabbed dialog (`Routes \| SOCKS \| Port forwards`) directly on any live session. |
| **Credentials, listeners & loot** | Review workspace credentials and loot, launch targeted login workflows, and track engine-registered payload listeners across restarts. |
| **Armitage-parity team mode** | Share one `msfrpcd` campaign across multiple operators with private Metasploit consoles, shared refcounted session transcripts, attributed events, team chat, optional TLS, and per-operator bound tokens. |
| **Persistence** | File-backed state in your user config directory (`--data-dir` / `--no-persist`): JSONL audit log with rotation, cached module ranks, listener registry, and saved connection profiles (passwords never stored). |
| **Reporting** | Export a clean, self-contained, print-ready HTML campaign report scoped to the active workspace — zero external assets, strict HTML escaping, team chat automatically excluded. |

![Hayduk 1.0.0 walkthrough: inspect Linux and Windows hosts, interact with Meterpreter, open Pivot Studio, search modules with Ctrl+K, and expand Topology Focus mode](docs/demo.gif)

*Walkthrough recorded with illustrative campaign data.*

---

## Quickstart

You need a Hayduk release binary, a modern browser, and a running Metasploit Framework instance. For workspace features (hosts, services, credentials, loot), Metasploit also needs a connected PostgreSQL database.

If you want a disposable Metasploit setup with a database and practice targets, skip to [Try the Docker lab](#try-the-docker-lab).

### 1. Download Hayduk

Grab the archive for your OS and architecture from the [latest release](https://github.com/jolovicdev/hayduk/releases/latest) and extract it. There is no installer or separate frontend build.

### 2. Start Metasploit RPC

On the machine running Metasploit:

```bash
msfrpcd -P 'yourpassword' -S -f -a 127.0.0.1 -p 55553
```

Replace `yourpassword` with your own password and keep this terminal open. `-a 127.0.0.1` restricts RPC to localhost; `-S` disables SSL for that local link.

### 3. Start Hayduk

From the extracted folder:

```bash
# Linux / macOS
./hayduk

# Windows PowerShell
.\hayduk.exe
```

Hayduk binds to localhost on an ephemeral port, generates a one-time session token, and opens the UI in your browser. If the browser does not open automatically, copy the full `http://127.0.0.1:<port>/?token=...` URL printed in the terminal.

### 4. Connect

In **Connect to msfrpcd**, enter your RPC settings (successful non-secret profiles are saved for one-click reuse):

| Field | Value |
|---|---|
| Host | `127.0.0.1` |
| Port | `55553` |
| User | `msf` |
| Password | The password you set in step 2 |
| Use SSL | Unchecked (for `-S`) |

Click **Connect**. A cold Metasploit daemon can take 15–30 seconds to respond; live progress is shown in the dialog. Press `Ctrl+C` in the Hayduk terminal to stop the server.

---

## Your first campaign

Use Hayduk only on systems and networks you own or have explicit written permission to test.

1. **Select a workspace.** Click the workspace picker in the top bar to switch Metasploit workspaces. Hosts, services, topology, and exported reports are scoped to the active workspace.
2. **Discover hosts.** Click **+ Discover** (or **Campaign → Discover hosts…**), enter a target host or CIDR range, pick a discovery module, review options, and launch.
3. **Fingerprint services.** Click **Scan** (or **Campaign → Scan services…**). Version detection runs `db_nmap -sV` by default and imports service fingerprints directly into the workspace; clear it to fall back to a Metasploit portscan module. Track progress in **Operations**.
4. **Launch modules & open sessions.** Press `Ctrl+K`, browse the **Modules** tree, or click **Attacks** in the Inspector to match exploits against a host's open services. When a session opens, click **Interact** on the host or select it in **Sessions** to attach.
5. **Pivot to internal subnets.** Click **Pivot** on a live session to add routes, start a SOCKS proxy, or configure port forwards. Routed subnets appear on the topology map as clickable cards that prefill **Scan services**.
6. **Export a report.** Click **Export** (or **File → Export report…**) to download a self-contained HTML campaign report ready for review or `Ctrl+P` PDF export.

![Hayduk 1.0.0 Topology Focus mode showing adaptive subnet groups, OS hardware tiles, host inspector, and a routed network](docs/topology.png)

*Topology Focus mode with illustrative campaign data.*

### Keyboard shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl+K` | Open Command Palette (modules, hosts, sessions, actions) |
| `1` / `2` / `3` | Switch between Topology, Services, and Hosts |
| `[` / `]` | Toggle Modules and Inspector side panels |
| `F` | Fit the topology graph to the viewport |
| `Tab` | Autocomplete commands in the Metasploit console |
| `Esc` | Close menus and dialogs |

---

## Try the Docker lab

The repository includes a disposable Metasploit + PostgreSQL lab with optional vulnerable targets so you can test discovery, scanning, pivoting, and reporting safely:

```bash
git clone https://github.com/jolovicdev/hayduk.git
cd hayduk
scripts/msf/up.sh --with-vulnbox
```

Connect Hayduk to `127.0.0.1:55553` with user `msf`, password `testpass123`, and **Use SSL** unchecked. Start without `--with-vulnbox` if you only want Metasploit and PostgreSQL.

Tear down the lab containers and volumes when finished:

```bash
scripts/msf/down.sh
```

---

## Team mode

Start Hayduk with `--team` and an explicit non-loopback interface address your operators can reach (wildcard binds like `0.0.0.0` are refused):

```bash
./hayduk --team --listen 192.168.1.10:8787
```

For engagements over HTTPS with verified per-operator identities, pass a TLS keypair and repeatable `--operator name=token` flags (tokens must be at least 16 characters and unique):

```bash
./hayduk --team --listen 192.168.1.10:8787 \
  --tls-cert server.crt --tls-key server.key \
  --operator milo=<16+ char secret> \
  --operator ana=<16+ char secret>
```

How shared campaigns work:

- **One shared backend.** Every operator works against the same `msfrpcd` link and workspace database: hosts, services, credentials, loot, operations, listeners, sessions, and pivot routes stay live for the whole team.
- **Private consoles.** Each operator receives their own Metasploit console; commands and scrollback never collide across operators.
- **Shared session transcripts.** Attach or detach any session tab independently. Transcripts and live output are shared and refcounted across viewers, so one operator closing a tab never interrupts another.
- **Attributed events & internal team chat.** Actions record which operator ran them. Team chat lives in the **Events** dock and is automatically excluded from exported client reports.

Read [SECURITY.md](SECURITY.md) for the full threat model and HTTP security headers (CSP, `X-Frame-Options: DENY`, `HttpOnly`/`Secure` token cookies, constant-time token verification).

---

## CLI flags

| Flag | Default | Description |
|---|---|---|
| `--listen` | `127.0.0.1:0` | Address `host:port` to bind. Required (non-loopback, non-wildcard) with `--team`. |
| `--team` | `false` | Enable multi-operator shared campaign mode. |
| `--operator` | *(none)* | Repeatable `name=token` binding an operator identity to a secret token (team mode, min 16 chars). |
| `--tls-cert` / `--tls-key` | *(none)* | PEM certificate and key files to serve the UI and WebSocket over HTTPS/WSS with `Secure` cookies. |
| `--data-dir` | OS config dir | Directory for `events.jsonl`, cached module ranks, listener registry, and saved profiles. |
| `--no-persist` | `false` | Run completely in memory without writing state to disk. |
| `--no-browser` | `false` | Do not open a browser window on startup. |
| `--dev` / `--dev-upstream` | `false` / `127.0.0.1:5173` | Proxy UI requests to a Vite dev server during frontend development. |
| `--version` | `false` | Print version and exit. |

---

## Troubleshooting

| Problem | What to check |
|---|---|
| Browser does not open | Copy the full URL with `?token=...` printed by Hayduk, or run with `--no-browser`. |
| Cannot connect to `msfrpcd` | Verify `msfrpcd` is running and host, port, user, password, and SSL toggle match. |
| SSL handshake error | Uncheck **Use SSL** when `msfrpcd` runs with `-S`; enable it when `msfrpcd` uses TLS. |
| Slow initial connection | A cold Metasploit daemon takes ~15–30s to initialize; watch progress in the Connect dialog. |
| Missing hosts or services | Verify the active workspace, Metasploit's PostgreSQL connection, and target reachability from the Metasploit host. |
| Service version scan fails | Version detection runs `db_nmap`, which requires `nmap` and a connected database on the Metasploit host. Uncheck version detection to use a portscan module. |

---

## FAQ

### Is Hayduk an Armitage alternative?
Yes. Hayduk implements the graphical Metasploit workflow popularized by Armitage — topology map, module tree, session consoles, pivoting, Hail Mary, and team campaigns — as a modern single Go binary with a browser UI and no Java runtime.

### Does Hayduk bundle Metasploit Framework?
No. Hayduk is a client for Metasploit's `msfrpcd` service. Use your existing Metasploit installation or the included Docker lab.

### Do I need Go, Node.js, or Java to run Hayduk?
No. Prebuilt release binaries have the entire UI embedded. Go and Node.js are only required if you build from source.

### Is Hayduk free and open source?
Yes — released under the [MIT License](LICENSE).

---

## Build from source

Requires Go 1.26+, Node.js 22/24, npm, and Make:

```bash
make
./bin/hayduk
```

### Development & checks

```bash
# Terminal 1: Vite UI dev server
cd ui && npm run dev

# Terminal 2: Go server proxying to Vite
make dev

# Verification gates
make test          # Go unit tests + Vitest UI suite
go test -race ./... # Full Go race detector pass
npm --prefix ui run lint
make gen-check     # Verify Go -> TypeScript protocol bindings (tygo)
make integration   # Requires running Docker lab
```

After adding Phosphor icons in `ui/src`, run `scripts/subset-icons.sh` (requires Python `fonttools` + `brotli`) to rebuild the minimal `.woff2` icon subset before `make`.

---

## Credits & license

Hayduk draws on the graphical attack management workflow established by [Armitage](https://github.com/rsmudge/armitage) by Raphael Mudge and communicates with Metasploit through [go-msf](https://github.com/jolovicdev/go-msf).

Licensed under [MIT](LICENSE). See [docs/THIRD-PARTY-NOTICES.md](docs/THIRD-PARTY-NOTICES.md) for bundled assets and font licenses.
