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
    --ink: #141413;
    --ink-2: #46443f;
    --ink-3: #757168;
    --paper: #ffffff;
    --tint: #f7f6f3;
    --rule: #e2dfd7;
    --rule-strong: #141413;
    --red: #b42318;
    --amber: #8a5300;
    --green: #146c43;
    --mono: ui-monospace, "SFMono-Regular", "Cascadia Mono", Menlo, Consolas, monospace;
    --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; background: var(--paper); color: var(--ink); font: 13.5px/1.6 var(--sans); -webkit-font-smoothing: antialiased; }
  main { max-width: 980px; margin: 0 auto; padding: 48px 36px 72px; }

  /* Document header — plain Swiss technical typography */
  .doc-head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: flex-end; gap: 20px; padding-bottom: 18px; border-bottom: 2px solid var(--rule-strong); }
  .doc-kicker { font: 600 11px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: var(--ink-3); margin: 0 0 8px; }
  h1 { font-size: 26px; font-weight: 700; letter-spacing: -.02em; margin: 0; line-height: 1.15; }
  .meta { color: var(--ink-2); font-size: 12.5px; margin: 8px 0 0; }
  .meta b { color: var(--ink); font-weight: 600; }
  .doc-class { font: 600 11px/1 var(--mono); letter-spacing: .08em; text-transform: uppercase; color: var(--ink-2); }

  /* Summary ledger — single ruled strip, no cards or top borders */
  .ledger { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); border-bottom: 1px solid var(--rule); margin: 0; }
  .ledger-cell { padding: 16px 14px 15px 0; border-right: 1px solid var(--rule); padding-left: 14px; }
  .ledger-cell:first-child { padding-left: 0; }
  .ledger-cell:last-child { border-right: 0; }
  .ledger-val { font: 600 22px/1.1 var(--mono); font-variant-numeric: tabular-nums; color: var(--ink); }
  .ledger-val.is-red { color: var(--red); }
  .ledger-lbl { font: 500 11px/1.2 var(--mono); text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); margin-top: 5px; }

  /* Inline table of contents */
  .toc { display: flex; flex-wrap: wrap; gap: 18px; padding: 12px 0; border-bottom: 1px solid var(--rule); font-size: 12.5px; }
  .toc a { color: var(--ink-2); text-decoration: none; }
  .toc a:hover { color: var(--ink); text-decoration: underline; }
  .toc a span { font-family: var(--mono); font-size: 11px; color: var(--ink-3); margin-left: 4px; }

  /* Sections */
  section { margin-top: 38px; }
  .sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; padding-bottom: 8px; border-bottom: 1px solid var(--rule-strong); margin-bottom: 2px; }
  h2 { font-size: 13px; font-weight: 700; margin: 0; text-transform: uppercase; letter-spacing: .09em; color: var(--ink); }
  .sec-num { font-family: var(--mono); font-weight: 600; color: var(--ink-3); margin-right: 8px; }
  .sec-count { font: 500 11.5px/1 var(--mono); color: var(--ink-3); }
  .sensitive { font: 600 11px/1 var(--mono); text-transform: uppercase; letter-spacing: .06em; color: var(--red); margin-left: 6px; }
  .notice { margin: 12px 0 6px; padding: 8px 12px; border-left: 2px solid var(--red); background: var(--tint); color: var(--ink-2); font-size: 12.5px; }

  /* Tables directly on the page */
  .table-wrap { width: 100%; overflow-x: auto; }
  table { border-collapse: collapse; width: 100%; font-size: 12.5px; }
  thead th { text-align: left; color: var(--ink-3); font: 600 10.5px/1.2 var(--mono); text-transform: uppercase; letter-spacing: .07em; padding: 10px 12px 9px 0; border-bottom: 1px solid var(--rule); white-space: nowrap; }
  tbody td { padding: 9px 12px 9px 0; border-bottom: 1px solid var(--rule); vertical-align: top; color: var(--ink); }
  tbody tr:last-child td { border-bottom: 0; }
  td.m, th.m { font-family: var(--mono); font-size: 12px; font-variant-numeric: tabular-nums; }
  td.wrap { overflow-wrap: anywhere; max-width: 340px; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; padding-right: 16px; }
  .empty-note { margin: 14px 0 0; color: var(--ink-3); font-size: 13px; }

  /* Restrained typographic status labels */
  .access { font: 600 11px/1 var(--mono); letter-spacing: .05em; text-transform: uppercase; }
  .access.yes { color: var(--red); }
  .access.cred { color: var(--amber); }
  .access.no { color: var(--ink-3); font-weight: 500; }

  .lvl { font: 600 11px/1 var(--mono); letter-spacing: .04em; text-transform: uppercase; }
  .lvl-error { color: var(--red); }
  .lvl-success { color: var(--green); }
  .lvl-warn { color: var(--amber); }
  .lvl-info { color: var(--ink-3); }

  footer { margin-top: 56px; padding-top: 14px; border-top: 1px solid var(--rule); display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; color: var(--ink-3); font-size: 11.5px; }

  @media (max-width: 760px) {
    main { padding: 28px 18px 48px; }
    .ledger { grid-template-columns: repeat(3, minmax(0, 1fr)); }
    .ledger-cell { border-right: 0; border-bottom: 1px solid var(--rule); padding: 12px 8px 12px 0; }
  }
  @media print {
    @page { margin: 16mm; }
    main { max-width: none; padding: 0; }
    .toc { display: none; }
    .sec-head { break-after: avoid; }
    tbody tr { break-inside: avoid; }
  }
</style>
</head>
<body>
<main>
  <header class="doc-head">
    <div>
      <p class="doc-kicker">Hayduk · Security Assessment Deliverable</p>
      <h1>Hayduk campaign report</h1>
      <p class="meta">Generated {{.Generated}} · workspace <b>{{.Workspace}}</b>{{if .MSFVersion}} · metasploit {{.MSFVersion}}{{end}}</p>
    </div>
    <span class="doc-class">Confidential</span>
  </header>

  <div class="ledger">
    <div class="ledger-cell"><div class="ledger-val">{{.HostCount}}</div><div class="ledger-lbl">Hosts</div></div>
    <div class="ledger-cell"><div class="ledger-val{{if gt .AccessCount 0}} is-red{{end}}">{{.AccessCount}}</div><div class="ledger-lbl">Access</div></div>
    <div class="ledger-cell"><div class="ledger-val">{{.ServiceCount}}</div><div class="ledger-lbl">Services</div></div>
    <div class="ledger-cell"><div class="ledger-val">{{.SessionCount}}</div><div class="ledger-lbl">Sessions</div></div>
    <div class="ledger-cell"><div class="ledger-val">{{.CredCount}}</div><div class="ledger-lbl">Credentials</div></div>
    <div class="ledger-cell"><div class="ledger-val">{{.LootCount}}</div><div class="ledger-lbl">Loot</div></div>
  </div>

  <nav class="toc" aria-label="Report sections">
    <a href="#hosts">01 Hosts<span>{{.HostCount}}</span></a>
    <a href="#services">02 Services<span>{{.ServiceCount}}</span></a>
    <a href="#sessions">03 Sessions<span>{{.SessionCount}}</span></a>
    <a href="#credentials">04 Credentials<span>{{.CredCount}}</span></a>
    <a href="#loot">05 Loot<span>{{.LootCount}}</span></a>
    <a href="#timeline">06 Event log<span>{{len .Events}}</span></a>
  </nav>

  <section id="hosts">
    <div class="sec-head">
      <h2><span class="sec-num">01</span>Hosts</h2>
      <span class="sec-count">{{.HostCount}} recorded</span>
    </div>
    {{if .Hosts}}<div class="table-wrap"><table>
      <thead><tr><th>Address</th><th>Hostname</th><th>Operating system</th><th class="num">Services</th><th>Access</th></tr></thead>
      <tbody>{{range .Hosts}}<tr>
        <td class="m">{{.Address}}</td>
        <td>{{if .Name}}{{.Name}}{{else}}—{{end}}</td>
        <td>{{if .OS}}{{.OS}}{{else}}—{{end}}</td>
        <td class="m num">{{.Services}}</td>
        <td>{{if .Access}}<span class="access yes">ACCESS</span>{{else if .Cred}}<span class="access cred">LOGIN</span>{{else}}<span class="access no">none</span>{{end}}</td>
      </tr>{{end}}</tbody>
    </table></div>{{else}}<p class="empty-note">No hosts recorded.</p>{{end}}
  </section>

  <section id="services">
    <div class="sec-head">
      <h2><span class="sec-num">02</span>Services</h2>
      <span class="sec-count">{{.ServiceCount}} recorded</span>
    </div>
    {{if .Services}}<div class="table-wrap"><table>
      <thead><tr><th>Host</th><th>Port</th><th>Proto</th><th>Service</th><th>Fingerprint / Details</th></tr></thead>
      <tbody>{{range .Services}}<tr>
        <td class="m">{{.Host}}</td>
        <td class="m">{{.Port}}</td>
        <td class="m">{{.Proto}}</td>
        <td>{{.Name}}</td>
        <td class="wrap">{{if .Info}}{{.Info}}{{else}}—{{end}}</td>
      </tr>{{end}}</tbody>
    </table></div>{{else}}<p class="empty-note">No services recorded.</p>{{end}}
  </section>

  <section id="sessions">
    <div class="sec-head">
      <h2><span class="sec-num">03</span>Sessions</h2>
      <span class="sec-count">{{.SessionCount}} open at export</span>
    </div>
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
    </table></div>{{else}}<p class="empty-note">No sessions were open at export time.</p>{{end}}
  </section>

  <section id="credentials">
    <div class="sec-head">
      <h2><span class="sec-num">04</span>Credentials <span class="sensitive">(sensitive)</span></h2>
      <span class="sec-count">{{.CredCount}} recovered</span>
    </div>
    {{if .Creds}}<p class="notice">Cleartext passwords and hashes below are sensitive assessment artifacts. Redact or encrypt before wider distribution.</p>
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
    </table></div>{{else}}<p class="empty-note">No credentials recovered.</p>{{end}}
  </section>

  <section id="loot">
    <div class="sec-head">
      <h2><span class="sec-num">05</span>Loot</h2>
      <span class="sec-count">{{.LootCount}} items</span>
    </div>
    {{if .Loot}}<div class="table-wrap"><table>
      <thead><tr><th>Host</th><th>Type</th><th>Name</th><th>Info</th></tr></thead>
      <tbody>{{range .Loot}}<tr>
        <td class="m">{{.Host}}</td>
        <td>{{.Type}}</td>
        <td>{{.Name}}</td>
        <td class="wrap">{{.Info}}</td>
      </tr>{{end}}</tbody>
    </table></div>{{else}}<p class="empty-note">No loot collected.</p>{{end}}
  </section>

  <section id="timeline">
    <div class="sec-head">
      <h2><span class="sec-num">06</span>Event log</h2>
      <span class="sec-count">{{len .Events}} entries</span>
    </div>
    {{if .Events}}<div class="table-wrap"><table>
      <thead><tr><th>Time</th><th>Level</th><th>Operator</th><th>Event</th></tr></thead>
      <tbody>{{range .Events}}<tr>
        <td class="m">{{.Time}}</td>
        <td><span class="lvl lvl-{{.Level}}">{{.Level}}</span></td>
        <td class="m">{{if .Operator}}{{.Operator}}{{else}}—{{end}}</td>
        <td class="wrap">{{.Text}}</td>
      </tr>{{end}}</tbody>
    </table></div>{{else}}<p class="empty-note">No events recorded.</p>{{end}}
  </section>

  <footer>
    <span>Generated by Hayduk. For authorized security testing only; handle credentials as sensitive material.</span>
    <span>Self-contained HTML · Ctrl+P to save PDF</span>
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
