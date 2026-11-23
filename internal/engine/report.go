package engine

import (
	"bytes"
	"html/template"
	"sort"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// report.html renders the campaign as one self-contained document: inline
// CSS, no external requests, html/template escaping throughout; hostile
// strings from the database must not execute in the reader's browser.

var reportTmpl = template.Must(template.New("report").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Hayduk campaign report{{if .Workspace}} — {{.Workspace}}{{end}}</title>
<style>
  :root {
    color-scheme: light;
    --ink: #161614;
    --ink-2: #4a4843;
    --ink-3: #76726a;
    --paper: #f6f5f2;
    --card: #ffffff;
    --line: #e4e1da;
    --line-strong: #d1cdc3;
    --masthead: #141413;
    --accent: #ff6b2b;
    --crimson: #c2271e;
    --crimson-bg: #fdeceb;
    --amber: #9a5b08;
    --amber-bg: #fef4e0;
    --emerald: #137347;
    --emerald-bg: #e7f6ee;
    --slate-bg: #f1efe9;
    --mono: ui-monospace, "SFMono-Regular", "Cascadia Mono", Menlo, Consolas, monospace;
    --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; background: var(--paper); color: var(--ink); font: 13.5px/1.55 var(--sans); -webkit-font-smoothing: antialiased; }

  /* Executive masthead */
  .masthead { background: var(--masthead); color: #f4f1ea; border-bottom: 3px solid var(--accent); }
  .masthead-inner { max-width: 1060px; margin: 0 auto; padding: 28px 32px 26px; display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: 20px; }
  .brand-row { display: flex; align-items: center; gap: 14px; }
  .brand-mark { width: 34px; height: 34px; border-radius: 6px; background: var(--accent); display: grid; place-items: center; flex: none; }
  .brand-kicker { font: 600 10.5px/1 var(--mono); letter-spacing: .12em; text-transform: uppercase; color: #ff9a6c; margin-bottom: 6px; }
  h1 { font-size: 24px; font-weight: 700; letter-spacing: -.015em; margin: 0; line-height: 1.15; color: #ffffff; }
  .meta { color: #b7b1a6; font-size: 12.5px; margin: 6px 0 0; }
  .meta b { color: #ffffff; font-weight: 600; }
  .class-pill { display: inline-flex; align-items: center; gap: 7px; padding: 5px 11px; border-radius: 4px; border: 1px solid rgba(255, 107, 43, .45); background: rgba(255, 107, 43, .14); color: #ffb494; font: 600 10.5px/1 var(--mono); letter-spacing: .09em; text-transform: uppercase; }
  .class-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); }

  main { max-width: 1060px; margin: 0 auto; padding: 28px 32px 64px; }

  /* Quick navigation */
  .toc { display: flex; flex-wrap: wrap; gap: 6px; margin: 0 0 24px; }
  .toc a { text-decoration: none; color: var(--ink-2); background: var(--card); border: 1px solid var(--line); border-radius: 5px; padding: 5px 11px; font: 500 12px/1 var(--sans); transition: border-color .12s, color .12s; }
  .toc a:hover { border-color: var(--accent); color: var(--ink); }
  .toc a span { font-family: var(--mono); font-size: 11px; color: var(--ink-3); margin-left: 5px; }

  /* Executive summary metrics */
  .summary-grid { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 12px; margin-bottom: 16px; }
  .metric { background: var(--card); border: 1px solid var(--line); border-top: 3px solid var(--line-strong); border-radius: 6px; padding: 14px 16px 13px; }
  .metric.hi { border-top-color: var(--crimson); }
  .metric.warn { border-top-color: var(--amber); }
  .metric.brand { border-top-color: var(--accent); }
  .metric-val { font: 700 24px/1.1 var(--mono); font-variant-numeric: tabular-nums; color: var(--ink); letter-spacing: -.02em; }
  .metric-lbl { font-size: 11.5px; font-weight: 600; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); margin-top: 6px; }

  /* Coverage bar */
  .coverage { background: var(--card); border: 1px solid var(--line); border-radius: 6px; padding: 14px 18px; margin-bottom: 28px; }
  .cov-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; margin-bottom: 9px; font-size: 12px; }
  .cov-title { font-weight: 600; color: var(--ink); text-transform: uppercase; letter-spacing: .06em; font-size: 11px; }
  .cov-legend { display: flex; flex-wrap: wrap; gap: 14px; color: var(--ink-2); font-size: 12px; }
  .cov-legend i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 5px; vertical-align: middle; }
  .cov-bar { height: 8px; border-radius: 4px; background: var(--slate-bg); overflow: hidden; display: flex; }
  .cov-seg.access { background: var(--crimson); }
  .cov-seg.login { background: #d98216; }

  /* Sections */
  section { margin-top: 30px; }
  .sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin: 0 0 10px; border-bottom: 1px solid var(--line-strong); padding-bottom: 7px; }
  h2 { font-size: 14px; font-weight: 700; margin: 0; text-transform: uppercase; letter-spacing: .08em; color: var(--ink); display: flex; align-items: center; gap: 8px; }
  .sec-num { font: 600 11.5px/1 var(--mono); color: var(--accent); }
  .sec-count { font: 500 11.5px/1 var(--mono); color: var(--ink-3); }

  .card { background: var(--card); border: 1px solid var(--line); border-radius: 7px; overflow: hidden; }
  .card-pad { padding: 16px 20px; }

  .notice { display: flex; align-items: flex-start; gap: 10px; padding: 11px 16px; background: #fff8f6; border-bottom: 1px solid #f5d3cf; color: #8c1d18; font-size: 12.5px; }
  .notice b { font-weight: 700; }
  .sensitive { display: inline-block; font: 700 10px/1 var(--mono); letter-spacing: .07em; text-transform: uppercase; color: var(--crimson); background: var(--crimson-bg); border: 1px solid #f5c6c3; border-radius: 3px; padding: 3px 6px; vertical-align: middle; }

  /* Tables */
  .table-wrap { width: 100%; overflow-x: auto; }
  table { border-collapse: collapse; width: 100%; font-size: 12.5px; }
  thead th { text-align: left; color: var(--ink-3); font: 600 10.5px/1.2 var(--mono); text-transform: uppercase; letter-spacing: .07em; padding: 10px 14px; background: #faf9f6; border-bottom: 1px solid var(--line); white-space: nowrap; }
  tbody td { padding: 9px 14px; border-bottom: 1px solid var(--line); vertical-align: top; color: var(--ink); }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:hover { background: #fbfaf7; }
  td.m, th.m { font-family: var(--mono); font-size: 12px; font-variant-numeric: tabular-nums; }
  td.wrap { overflow-wrap: anywhere; max-width: 320px; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .empty-note { margin: 0; color: var(--ink-3); font-size: 13px; }

  /* Badges */
  .access { display: inline-block; font: 700 10px/1 var(--mono); letter-spacing: .06em; border-radius: 4px; padding: 4px 7px; text-transform: uppercase; }
  .access.yes { color: var(--crimson); background: var(--crimson-bg); border: 1px solid #f5c6c3; }
  .access.cred { color: var(--amber); background: var(--amber-bg); border: 1px solid #f2d79c; }
  .access.no { color: var(--ink-3); background: var(--slate-bg); border: 1px solid var(--line); }

  .lvl { display: inline-block; font: 700 10px/1 var(--mono); letter-spacing: .05em; text-transform: uppercase; border-radius: 3px; padding: 3px 6px; }
  .lvl-error { color: var(--crimson); background: var(--crimson-bg); }
  .lvl-success { color: var(--emerald); background: var(--emerald-bg); }
  .lvl-warn { color: var(--amber); background: var(--amber-bg); }
  .lvl-info { color: var(--ink-2); background: var(--slate-bg); }

  footer { margin-top: 44px; padding-top: 16px; border-top: 1px solid var(--line-strong); display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; color: var(--ink-3); font-size: 11.5px; }
  footer b { color: var(--ink-2); }

  @media (max-width: 820px) {
    .summary-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
    .masthead-inner, main { padding-left: 18px; padding-right: 18px; }
  }
  @media print {
    @page { margin: 14mm; }
    html, body { background: #ffffff; }
    .masthead { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
    .toc { display: none; }
    main { max-width: none; padding: 18px 0 0; }
    .card, .metric, .coverage, tbody tr { break-inside: avoid; }
    .sec-head { break-after: avoid; }
    .access, .lvl, .sensitive, .cov-seg { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  }
</style>
</head>
<body>
<header class="masthead">
  <div class="masthead-inner">
    <div class="brand-row">
      <div class="brand-mark" aria-hidden="true">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
          <path d="M5 4h4v6.5h6V4h4v16h-4v-6H9v6H5V4z" fill="#141413"/>
        </svg>
      </div>
      <div>
        <div class="brand-kicker">Security Assessment Deliverable</div>
        <h1>Hayduk campaign report</h1>
        <p class="meta">Generated {{.Generated}} · workspace <b>{{.Workspace}}</b>{{if .MSFVersion}} · metasploit {{.MSFVersion}}{{end}}</p>
      </div>
    </div>
    <div class="class-pill"><span class="class-dot"></span>Confidential · Authorized Assessment</div>
  </div>
</header>

<main>
  <nav class="toc" aria-label="Report sections">
    <a href="#hosts">Hosts<span>{{.HostCount}}</span></a>
    <a href="#services">Services<span>{{.ServiceCount}}</span></a>
    <a href="#sessions">Sessions<span>{{.SessionCount}}</span></a>
    <a href="#credentials">Credentials<span>{{.CredCount}}</span></a>
    <a href="#loot">Loot<span>{{.LootCount}}</span></a>
    <a href="#timeline">Event log<span>{{len .Events}}</span></a>
  </nav>

  <div class="summary-grid">
    <div class="metric brand"><div class="metric-val">{{.HostCount}}</div><div class="metric-lbl">Hosts</div></div>
    <div class="metric hi"><div class="metric-val">{{.AccessCount}}</div><div class="metric-lbl">Compromised</div></div>
    <div class="metric"><div class="metric-val">{{.ServiceCount}}</div><div class="metric-lbl">Open services</div></div>
    <div class="metric hi"><div class="metric-val">{{.SessionCount}}</div><div class="metric-lbl">Active sessions</div></div>
    <div class="metric warn"><div class="metric-val">{{.CredCount}}</div><div class="metric-lbl">Credentials</div></div>
    <div class="metric"><div class="metric-val">{{.LootCount}}</div><div class="metric-lbl">Loot items</div></div>
  </div>

  {{if gt .HostCount 0}}<div class="coverage">
    <div class="cov-head">
      <span class="cov-title">Target access coverage</span>
      <span class="cov-legend">
        <span><i style="background:#c2271e"></i>Access ({{.AccessCount}})</span>
        <span><i style="background:#d98216"></i>Login available ({{.LoginCount}})</span>
        <span><i style="background:#d1cdc3"></i>Discovered ({{.DiscoveredCount}})</span>
      </span>
    </div>
    <div class="cov-bar">
      {{if gt .AccessPct 0}}<div class="cov-seg access" style="width:{{.AccessPct}}%"></div>{{end}}
      {{if gt .LoginPct 0}}<div class="cov-seg login" style="width:{{.LoginPct}}%"></div>{{end}}
    </div>
  </div>{{end}}

  <section id="hosts">
    <div class="sec-head">
      <h2><span class="sec-num">01</span>Hosts</h2>
      <span class="sec-count">{{.HostCount}} recorded</span>
    </div>
    <div class="card">
      {{if .Hosts}}<div class="table-wrap"><table>
        <thead><tr><th>Address</th><th>Hostname</th><th>Operating system</th><th class="num">Services</th><th>Access</th></tr></thead>
        <tbody>{{range .Hosts}}<tr>
          <td class="m">{{.Address}}</td>
          <td>{{if .Name}}{{.Name}}{{else}}—{{end}}</td>
          <td>{{if .OS}}{{.OS}}{{else}}—{{end}}</td>
          <td class="m num">{{.Services}}</td>
          <td>{{if .Access}}<span class="access yes">ACCESS</span>{{else if .Cred}}<span class="access cred">LOGIN</span>{{else}}<span class="access no">none</span>{{end}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No hosts recorded.</p></div>{{end}}
    </div>
  </section>

  <section id="services">
    <div class="sec-head">
      <h2><span class="sec-num">02</span>Services</h2>
      <span class="sec-count">{{.ServiceCount}} recorded</span>
    </div>
    <div class="card">
      {{if .Services}}<div class="table-wrap"><table>
        <thead><tr><th>Host</th><th>Port</th><th>Protocol</th><th>Service</th><th>Fingerprint / Details</th></tr></thead>
        <tbody>{{range .Services}}<tr>
          <td class="m">{{.Host}}</td>
          <td class="m">{{.Port}}</td>
          <td class="m">{{.Proto}}</td>
          <td>{{.Name}}</td>
          <td class="wrap">{{if .Info}}{{.Info}}{{else}}—{{end}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No services recorded.</p></div>{{end}}
    </div>
  </section>

  <section id="sessions">
    <div class="sec-head">
      <h2><span class="sec-num">03</span>Sessions</h2>
      <span class="sec-count">{{.SessionCount}} active at export</span>
    </div>
    <div class="card">
      {{if .Sessions}}<div class="table-wrap"><table>
        <thead><tr><th>ID</th><th>Type</th><th>User</th><th>Host</th><th>Via module</th><th>Opened</th></tr></thead>
        <tbody>{{range .Sessions}}<tr>
          <td class="m">{{.ID}}</td>
          <td>{{.Type}}</td>
          <td class="m">{{if .User}}{{.User}}{{else}}—{{end}}</td>
          <td class="m">{{.Host}}</td>
          <td class="m wrap">{{if .Via}}{{.Via}}{{else}}—{{end}}</td>
          <td class="m">{{.Opened}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No sessions were open at export time.</p></div>{{end}}
    </div>
  </section>

  <section id="credentials">
    <div class="sec-head">
      <h2><span class="sec-num">04</span>Credentials <span class="sensitive">sensitive</span></h2>
      <span class="sec-count">{{.CredCount}} recovered</span>
    </div>
    <div class="card">
      {{if .Creds}}<div class="notice"><b>Handling notice:</b> Cleartext passwords and hashes below are sensitive assessment artifacts. Redact or encrypt before wider distribution.</div>
      <div class="table-wrap"><table>
        <thead><tr><th>Host</th><th>Port</th><th>Service</th><th>User</th><th>Password / hash</th><th>Type</th></tr></thead>
        <tbody>{{range .Creds}}<tr>
          <td class="m">{{.Host}}</td>
          <td class="m">{{.Port}}</td>
          <td>{{.Service}}</td>
          <td class="m">{{.User}}</td>
          <td class="m wrap">{{.Pass}}</td>
          <td>{{.Type}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No credentials recovered.</p></div>{{end}}
    </div>
  </section>

  <section id="loot">
    <div class="sec-head">
      <h2><span class="sec-num">05</span>Loot</h2>
      <span class="sec-count">{{.LootCount}} items</span>
    </div>
    <div class="card">
      {{if .Loot}}<div class="table-wrap"><table>
        <thead><tr><th>Host</th><th>Type</th><th>Name</th><th>Info</th></tr></thead>
        <tbody>{{range .Loot}}<tr>
          <td class="m">{{.Host}}</td>
          <td>{{.Type}}</td>
          <td>{{.Name}}</td>
          <td class="wrap">{{.Info}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No loot collected.</p></div>{{end}}
    </div>
  </section>

  <section id="timeline">
    <div class="sec-head">
      <h2><span class="sec-num">06</span>Event log</h2>
      <span class="sec-count">{{len .Events}} entries</span>
    </div>
    <div class="card">
      {{if .Events}}<div class="table-wrap"><table>
        <thead><tr><th>Time</th><th>Level</th><th>Operator</th><th>Event</th></tr></thead>
        <tbody>{{range .Events}}<tr>
          <td class="m">{{.Time}}</td>
          <td><span class="lvl lvl-{{.Level}}">{{.Level}}</span></td>
          <td class="m">{{if .Operator}}{{.Operator}}{{else}}—{{end}}</td>
          <td class="wrap">{{.Text}}</td>
        </tr>{{end}}</tbody>
      </table></div>{{else}}<div class="card-pad"><p class="empty-note">No events recorded.</p></div>{{end}}
    </div>
  </section>

  <footer>
    <span>Generated by <b>Hayduk</b>. For authorized security testing only; handle credentials as sensitive material.</span>
    <span>Tip: Print to PDF (Ctrl+P) for distribution</span>
  </footer>
</main>
</body>
</html>
`))

type reportHost struct {
	Address  string
	Name     string
	OS       string
	Services int
	Access   bool
	Cred     bool
}

type reportService struct {
	Host, Proto, Name, Info string
	Port                    int
}

type reportSession struct {
	ID, Type, User, Host, Via, Opened string
}

type reportCred struct {
	Host, Service, User, Pass, Type string
	Port                            int
}

type reportLoot struct {
	Host, Type, Name, Info string
}

type reportEvent struct {
	Time, Level, Operator, Text string
}

type reportData struct {
	Generated, Workspace, MSFVersion      string
	HostCount, ServiceCount, SessionCount int
	CredCount, LootCount                  int
	AccessCount, LoginCount               int
	DiscoveredCount                       int
	AccessPct, LoginPct                   int
	Hosts                                 []reportHost
	Services                              []reportService
	Sessions                              []reportSession
	Creds                                 []reportCred
	Loot                                  []reportLoot
	Events                                []reportEvent
}

func (e *Engine) reportDocument() (string, *protocol.ErrorBody) {
	s := e.State()

	svcByHost := map[string]int{}
	for _, svc := range s.Services {
		if svc != nil {
			svcByHost[svc.Host]++
		}
	}
	sessByHost := map[string]bool{}
	for _, sess := range s.Sessions {
		if sess == nil {
			continue
		}
		if h := sess.TargetHost; h != "" {
			sessByHost[h] = true
		} else if h := sess.SessionHost; h != "" {
			sessByHost[h] = true
		}
	}
	credByHost := map[string]bool{}
	for _, c := range s.Creds {
		if c != nil {
			credByHost[c.Host] = true
		}
	}

	data := reportData{
		Generated:    time.Now().UTC().Format(time.RFC3339),
		Workspace:    s.Connection.Workspace,
		MSFVersion:   s.Connection.MSFVersion,
		HostCount:    len(s.Hosts),
		ServiceCount: len(s.Services),
		SessionCount: len(s.Sessions),
		CredCount:    len(s.Creds),
		LootCount:    len(s.Loot),
	}
	for _, h := range s.Hosts {
		if h == nil {
			continue
		}
		hasAccess := sessByHost[h.Address]
		hasCred := credByHost[h.Address]
		if hasAccess {
			data.AccessCount++
		} else if hasCred {
			data.LoginCount++
		} else {
			data.DiscoveredCount++
		}
		data.Hosts = append(data.Hosts, reportHost{
			Address: h.Address, Name: h.Name,
			OS:       joinNonEmpty(h.OSName, h.OSFlavor, h.OSVersion),
			Services: svcByHost[h.Address],
			Access:   hasAccess,
			Cred:     hasCred,
		})
	}
	if len(data.Hosts) > 0 {
		data.AccessPct = (data.AccessCount * 100) / len(data.Hosts)
		data.LoginPct = (data.LoginCount * 100) / len(data.Hosts)
	}
	sort.Slice(data.Hosts, func(i, j int) bool { return data.Hosts[i].Address < data.Hosts[j].Address })
	for _, v := range s.Services {
		if v == nil {
			continue
		}
		data.Services = append(data.Services, reportService{Host: v.Host, Port: v.Port, Proto: v.Proto, Name: v.Name, Info: v.Info})
	}
	sort.Slice(data.Services, func(i, j int) bool {
		if data.Services[i].Host != data.Services[j].Host {
			return data.Services[i].Host < data.Services[j].Host
		}
		return data.Services[i].Port < data.Services[j].Port
	})
	// Sessions are framework-wide: each is scoped to the workspace it was
	// opened under. Sessions with no workspace tag (picked up before any
	// attribution) fall back to host membership.
	hostSet := make(map[string]bool, len(s.Hosts))
	for _, h := range s.Hosts {
		if h != nil {
			hostSet[h.Address] = true
		}
	}
	sessions := make([]*protocol.SessionState, 0, len(s.Sessions))
	for _, v := range s.Sessions {
		if v == nil {
			continue
		}
		switch {
		case v.Workspace != "":
			if v.Workspace != s.Connection.Workspace {
				continue
			}
		default:
			host := firstNonEmpty(v.TargetHost, v.SessionHost)
			if host == "" || !hostSet[host] {
				continue
			}
		}
		sessions = append(sessions, v)
	}
	data.SessionCount = len(sessions)
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	for _, v := range sessions {
		data.Sessions = append(data.Sessions, reportSession{
			ID: v.ID, Type: v.Type, User: v.Username,
			Host: firstNonEmpty(v.TargetHost, v.SessionHost),
			Via:  v.ViaExploit, Opened: formatReportTime(v.OpenedAt),
		})
	}
	for _, v := range s.Creds {
		if v == nil {
			continue
		}
		data.Creds = append(data.Creds, reportCred{Host: v.Host, Port: v.Port, Service: v.Service, User: v.User, Pass: v.Pass, Type: v.Type})
	}
	for _, v := range s.Loot {
		if v == nil {
			continue
		}
		data.Loot = append(data.Loot, reportLoot{Host: v.Host, Type: v.Type, Name: v.Name, Info: v.Info})
	}
	for _, v := range s.Events {
		if v == nil {
			continue
		}
		// team chat is internal coordination, not campaign history: it
		// never rides into a client report
		if v.Level == protocol.LevelChat {
			continue
		}
		// events inherit the workspace they were recorded under: one
		// client's history must never ride into another client's report.
		// Untagged entries are framework-wide and pre-connection ones.
		if v.Workspace != "" && v.Workspace != s.Connection.Workspace {
			continue
		}
		data.Events = append(data.Events, reportEvent{
			Time: formatReportTime(v.Time), Level: v.Level,
			Operator: v.Operator, Text: v.Text,
		})
	}

	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, data); err != nil {
		return "", &protocol.ErrorBody{Code: protocol.CodeInternal, Message: err.Error()}
	}
	return buf.String(), nil
}

func joinNonEmpty(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += p
	}
	return out
}

func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if p != "" {
			return p
		}
	}
	return ""
}

func formatReportTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
