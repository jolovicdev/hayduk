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
<title>Hayduk campaign report</title>
<style>
  :root { color-scheme: light; }
  body { font: 14px/1.55 -apple-system, "Segoe UI", Roboto, sans-serif; color: #1c2128; margin: 0; background: #f4f5f7; }
  main { max-width: 980px; margin: 0 auto; padding: 40px 28px 64px; }
  h1 { font-size: 22px; margin: 0 0 2px; letter-spacing: .02em; }
  h2 { font-size: 15px; margin: 34px 0 8px; text-transform: uppercase; letter-spacing: .08em; color: #57606a; }
  .meta { color: #57606a; font-size: 12.5px; }
  .meta b { color: #1c2128; }
  .card { background: #fff; border: 1px solid #d8dee4; border-radius: 10px; padding: 18px 20px; margin-top: 8px; }
  table { border-collapse: collapse; width: 100%; font-size: 12.5px; }
  th { text-align: left; color: #57606a; font-weight: 600; padding: 6px 10px 6px 0; border-bottom: 1px solid #d8dee4; }
  td { padding: 6px 10px 6px 0; border-bottom: 1px solid #eaeef2; vertical-align: top; }
  tr:last-child td { border-bottom: 0; }
  td.m, th.m { font-family: ui-monospace, "Cascadia Mono", Menlo, monospace; font-size: 11.5px; }
  .access { display: inline-block; font-size: 10.5px; font-weight: 700; letter-spacing: .05em; border-radius: 4px; padding: 1px 7px; }
  .access.yes { color: #b6230f; background: #ffe8e4; }
  .access.cred { color: #9a6700; background: #fff3d6; }
  .access.no { color: #57606a; background: #eef1f4; }
  .sensitive { color: #b6230f; font-weight: 700; }
  .counts { display: flex; gap: 26px; margin-top: 14px; }
  .counts div { font-size: 12px; color: #57606a; }
  .counts b { display: block; font-size: 21px; color: #1c2128; }
  .lvl-error { color: #b6230f; }
  .lvl-success { color: #1a7f37; }
  .lvl-warn { color: #9a6700; }
  footer { margin-top: 44px; color: #57606a; font-size: 11.5px; border-top: 1px solid #d8dee4; padding-top: 12px; }
</style>
</head>
<body>
<main>
  <h1>Hayduk campaign report</h1>
  <p class="meta">Generated {{.Generated}} · workspace <b>{{.Workspace}}</b> · metasploit {{.MSFVersion}}</p>
  <div class="counts">
    <div><b>{{.HostCount}}</b>hosts</div>
    <div><b>{{.ServiceCount}}</b>services</div>
    <div><b>{{.SessionCount}}</b>sessions</div>
    <div><b>{{.CredCount}}</b>credentials</div>
    <div><b>{{.LootCount}}</b>loot</div>
  </div>

  <h2>Hosts</h2>
  <div class="card">
    {{if .Hosts}}<table>
      <tr><th>Address</th><th>Name</th><th>Operating system</th><th>Services</th><th>Access</th></tr>
      {{range .Hosts}}<tr>
        <td class="m">{{.Address}}</td>
        <td>{{.Name}}</td>
        <td>{{.OS}}</td>
        <td>{{.Services}}</td>
        <td>{{if .Access}}<span class="access yes">ACCESS</span>{{else if .Cred}}<span class="access cred">LOGIN</span>{{else}}<span class="access no">none</span>{{end}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No hosts recorded.</p>{{end}}
  </div>

  <h2>Services</h2>
  <div class="card">
    {{if .Services}}<table>
      <tr><th>Host</th><th>Port</th><th>Protocol</th><th>Name</th><th>Details</th></tr>
      {{range .Services}}<tr>
        <td class="m">{{.Host}}</td>
        <td class="m">{{.Port}}</td>
        <td>{{.Proto}}</td>
        <td>{{.Name}}</td>
        <td>{{.Info}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No services recorded.</p>{{end}}
  </div>

  <h2>Sessions</h2>
  <div class="card">
    {{if .Sessions}}<table>
      <tr><th>ID</th><th>Type</th><th>User</th><th>Host</th><th>Via</th><th>Opened</th></tr>
      {{range .Sessions}}<tr>
        <td class="m">{{.ID}}</td>
        <td>{{.Type}}</td>
        <td>{{.User}}</td>
        <td class="m">{{.Host}}</td>
        <td class="m">{{.Via}}</td>
        <td>{{.Opened}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No sessions were open at export time.</p>{{end}}
  </div>

  <h2>Credentials <span class="sensitive">(sensitive)</span></h2>
  <div class="card">
    {{if .Creds}}<table>
      <tr><th>Host</th><th>Port</th><th>Service</th><th>User</th><th>Password / hash</th><th>Type</th></tr>
      {{range .Creds}}<tr>
        <td class="m">{{.Host}}</td>
        <td class="m">{{.Port}}</td>
        <td>{{.Service}}</td>
        <td>{{.User}}</td>
        <td class="m">{{.Pass}}</td>
        <td>{{.Type}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No credentials recovered.</p>{{end}}
  </div>

  <h2>Loot</h2>
  <div class="card">
    {{if .Loot}}<table>
      <tr><th>Host</th><th>Type</th><th>Name</th><th>Info</th></tr>
      {{range .Loot}}<tr>
        <td class="m">{{.Host}}</td>
        <td>{{.Type}}</td>
        <td>{{.Name}}</td>
        <td>{{.Info}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No loot collected.</p>{{end}}
  </div>

  <h2>Event log</h2>
  <div class="card">
    {{if .Events}}<table>
      <tr><th>Time</th><th></th><th>Event</th></tr>
      {{range .Events}}<tr>
        <td class="m">{{.Time}}</td>
        <td class="lvl-{{.Level}}">{{.Level}}</td>
        <td>{{.Text}}</td>
      </tr>{{end}}
    </table>{{else}}<p class="meta">No events recorded.</p>{{end}}
  </div>

  <footer>
    Generated by Hayduk. For authorized security testing only; handle credentials as sensitive material.
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
	Time, Level, Text string
}

type reportData struct {
	Generated, Workspace, MSFVersion                 string
	HostCount, ServiceCount, SessionCount            int
	CredCount, LootCount                             int
	Hosts                                            []reportHost
	Services                                         []reportService
	Sessions                                         []reportSession
	Creds                                            []reportCred
	Loot                                             []reportLoot
	Events                                           []reportEvent
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
		Generated:     time.Now().UTC().Format(time.RFC3339),
		Workspace:     s.Connection.Workspace,
		MSFVersion:    s.Connection.MSFVersion,
		HostCount:     len(s.Hosts),
		ServiceCount:  len(s.Services),
		SessionCount:  len(s.Sessions),
		CredCount:     len(s.Creds),
		LootCount:     len(s.Loot),
	}
	for _, h := range s.Hosts {
		if h == nil {
			continue
		}
		data.Hosts = append(data.Hosts, reportHost{
			Address: h.Address, Name: h.Name,
			OS:       joinNonEmpty(h.OSName, h.OSFlavor, h.OSVersion),
			Services: svcByHost[h.Address],
			Access:   sessByHost[h.Address],
			Cred:     credByHost[h.Address],
		})
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
	sessions := make([]*protocol.SessionState, 0, len(s.Sessions))
	for _, v := range s.Sessions {
		if v != nil {
			sessions = append(sessions, v)
		}
	}
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
		data.Events = append(data.Events, reportEvent{Time: formatReportTime(v.Time), Level: v.Level, Text: v.Text})
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
