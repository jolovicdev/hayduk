//go:build integration

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func integrationEnv(t *testing.T) protocol.ConnectParams {
	t.Helper()
	if os.Getenv("RUN_MSF_INTEGRATION") != "1" {
		t.Skip("RUN_MSF_INTEGRATION=1 not set")
	}
	return protocol.ConnectParams{
		Host:     envOr("MSF_HOST", "127.0.0.1"),
		Port:     55553,
		SSL:      false,
		Username: envOr("MSF_USERNAME", "msf"),
		Password: envOr("MSF_PASSWORD", "testpass123"),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestIntegrationConnectAndBootstrap(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	st := e.State()
	if st.Connection.Status != "connected" || st.Connection.MSFVersion == "" {
		t.Fatalf("connection %+v", st.Connection)
	}
	if st.Connection.Workspace != "default" {
		t.Fatalf("workspace %q", st.Connection.Workspace)
	}
	if st.Modules == nil || len(st.Modules.Exploits) < 500 {
		t.Fatalf("exploits %d", len(st.Modules.Exploits))
	}
	if st.Console == nil {
		t.Fatal("console missing")
	}
}

func TestIntegrationConsoleRoundtrip(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, err := e.Exec(context.Background(), "", protocol.MethodConsoleWrite,
		json.RawMessage(`{"command":"version\n"}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(30 * time.Second)
	for {
		if strings.Contains(e.State().Console.Output, "Framework:") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("version output never arrived")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

const testWS = "hayduk-integration"

// seedTestWorkspace fills a fresh workspace with 105 hosts and services:
// one row past the daemon's 100-row default read limit.
func seedTestWorkspace(t *testing.T) *gomsf.Client {
	t.Helper()
	p := integrationEnv(t)
	rpc, err := gomsf.NewClient(p.Password,
		gomsf.WithHost(p.Host), gomsf.WithPort(p.Port),
		gomsf.WithSSL(p.SSL), gomsf.WithUsername(p.Username))
	if err != nil {
		t.Fatalf("seed client login: %v", err)
	}
	ctx := context.Background()
	_, _ = rpc.Call(ctx, gomsf.DbSetWorkspace, "default")
	_, _ = rpc.Call(ctx, gomsf.DbDelWorkspace, testWS)
	if _, err := rpc.Call(ctx, gomsf.DbAddWorkspace, testWS); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	for i := 1; i <= 105; i++ {
		addr := fmt.Sprintf("192.0.2.%d", i)
		if _, err := rpc.Call(ctx, gomsf.DbReportHost, map[string]interface{}{
			"workspace": testWS, "host": addr, "os_name": "integration",
		}); err != nil {
			t.Fatalf("report_host %s: %v", addr, err)
		}
		if _, err := rpc.Call(ctx, gomsf.DbReportService, map[string]interface{}{
			"workspace": testWS, "host": addr, "port": 2222, "proto": "tcp", "name": "ssh",
		}); err != nil {
			t.Fatalf("report_service %s: %v", addr, err)
		}
	}
	return rpc
}

func dropTestWorkspace(rpc *gomsf.Client) {
	ctx := context.Background()
	_, _ = rpc.Call(ctx, gomsf.DbSetWorkspace, "default")
	_, _ = rpc.Call(ctx, gomsf.DbDelWorkspace, testWS)
}

func TestIntegrationRefreshFetchesAllPages(t *testing.T) {
	seedRPC := seedTestWorkspace(t)
	defer dropTestWorkspace(seedRPC)

	// the daemon caps a single unpaged read at 100 rows
	raw, err := gomsf.NewDbManager(seedRPC).Hosts(context.Background(),
		map[string]interface{}{"workspace": testWS})
	if err != nil {
		t.Fatalf("raw hosts read: %v", err)
	}
	if len(raw) != 100 {
		t.Fatalf("raw single read returned %d rows, want the daemon's 100 cap", len(raw))
	}

	e := New(Config{})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	if _, err := e.Exec(context.Background(), "", protocol.MethodWorkspaceSet,
		json.RawMessage(`{"name":"`+testWS+`"}`)); err != nil {
		t.Fatalf("workspace switch: %+v", err)
	}
	st := e.State()
	if len(st.Hosts) != 105 || len(st.Services) != 105 {
		t.Fatalf("105 rows seeded, campaign holds %d hosts and %d services", len(st.Hosts), len(st.Services))
	}
	found := false
	for _, h := range st.Hosts {
		if h.Address == "192.0.2.105" {
			found = true
		}
	}
	if !found {
		t.Fatal("host from the second page missing")
	}
}

func TestIntegrationReportWorkspaceScoped(t *testing.T) {
	seedRPC := seedTestWorkspace(t)
	defer dropTestWorkspace(seedRPC)

	e := New(Config{})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, err := e.Exec(context.Background(), "integration-op", protocol.MethodWorkspaceSet,
		json.RawMessage(`{"name":"`+testWS+`"}`)); err != nil {
		t.Fatalf("switch: %+v", err)
	}
	var payload protocol.ReportPayload
	raw, err := e.Exec(context.Background(), "", protocol.MethodReportHTML, nil)
	if err != nil {
		t.Fatalf("report in %s: %+v", testWS, err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.HTML, "192.0.2.105") {
		t.Fatal("report missing the second-page host")
	}
	if !strings.Contains(payload.HTML, "integration-op") {
		t.Fatal("report missing operator attribution")
	}

	if _, err := e.Exec(context.Background(), "", protocol.MethodWorkspaceSet,
		json.RawMessage(`{"name":"default"}`)); err != nil {
		t.Fatalf("switch back: %+v", err)
	}
	raw, err = e.Exec(context.Background(), "", protocol.MethodReportHTML, nil)
	if err != nil {
		t.Fatalf("report in default: %+v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload.HTML, "192.0.2.105") || strings.Contains(payload.HTML, "integration-op") {
		t.Fatal("default report contains test workspace data")
	}
}

func TestIntegrationConsoleWriteAnswersConsoleState(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, err := e.Exec(context.Background(), "", protocol.MethodConsoleWrite,
		json.RawMessage(`{"command":"\n"}`)); err != nil {
		t.Fatalf("write: %+v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-sub.C():
			up, ok := m.(protocol.ResourceUpdate)
			if ok && up.Resource == protocol.ResConsole {
				return
			}
		case <-deadline:
			t.Fatal("no console update after the write")
		}
	}
}

// Requires the lab's sshbox (msfadmin/msfadmin on port 2222) via
// MSF_SSH_TARGET.
func TestIntegrationSessionReattachAndWriteGuard(t *testing.T) {
	target := os.Getenv("MSF_SSH_TARGET")
	if target == "" {
		t.Skip("MSF_SSH_TARGET not set")
	}
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	_, eb := e.Exec(context.Background(), "integration-op", protocol.MethodModuleExecute, json.RawMessage(`{
		"type":"auxiliary","name":"scanner/ssh/ssh_login",
		"options":{"RHOSTS":"`+target+`","RPORT":2222,"USERNAME":"msfadmin","PASSWORD":"msfadmin","STOP_ON_SUCCESS":true}
	}`))
	if eb != nil {
		t.Fatalf("ssh_login launch: %+v", eb)
	}
	var sid string
	deadline := time.After(90 * time.Second)
	for sid == "" {
		for s := range e.State().Sessions {
			sid = s
		}
		if sid != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ssh_login never opened a session")
		case <-time.After(200 * time.Millisecond):
		}
	}

	attach := func() {
		if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionAttach,
			json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
			t.Fatalf("attach: %+v", eb)
		}
	}
	attach()
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionWrite,
		json.RawMessage(`{"sid":"`+sid+`","data":"echo integration-reattach\n"}`)); eb != nil {
		t.Fatalf("session write: %+v", eb)
	}
	deadline = time.After(30 * time.Second)
	for {
		if strings.Contains(e.State().Interact.Output, "integration-reattach") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("session output never arrived; transcript: %q", e.State().Interact.Output)
		case <-time.After(100 * time.Millisecond):
		}
	}

	attach()
	if !strings.Contains(e.State().Interact.Output, "integration-reattach") {
		t.Fatalf("reattach cleared the transcript: %q", e.State().Interact.Output)
	}

	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionDetach, nil); eb != nil {
		t.Fatalf("detach: %+v", eb)
	}
	_, eb = e.Exec(context.Background(), "", protocol.MethodSessionWrite,
		json.RawMessage(`{"sid":"`+sid+`","data":"echo must-not-run\n"}`))
	if eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("write after detach: got %+v, want busy", eb)
	}

	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionStop,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("stop session: %+v", eb)
	}
}

// Team parity against a live daemon: two operators, two private consoles.
// One operator's command and its output never reach the other's console.
func TestIntegrationTeamConsoleIsolation(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	e.OperatorJoin("dana")
	e.OperatorJoin("mile")
	waitFor(t, func() bool {
		return e.ConsoleFor("dana") != nil && e.ConsoleFor("mile") != nil
	})

	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodConsoleWrite,
		json.RawMessage(`{"command":"version\n"}`)); eb != nil {
		t.Fatalf("dana write: %+v", eb)
	}
	deadline := time.After(30 * time.Second)
	for {
		if strings.Contains(e.ConsoleFor("dana").Output, "Framework:") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("dana's version output never arrived")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if got := e.ConsoleFor("mile").Output; strings.Contains(got, "Framework:") {
		t.Fatalf("dana's command leaked into mile's console: %q", got)
	}
}

// Chat reaches every operator through the shared event log, attributed.
func TestIntegrationTeamChat(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	e.OperatorJoin("dana")
	e.OperatorJoin("mile")

	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodTeamChat,
		json.RawMessage(`{"text":"integration chat ping"}`)); eb != nil {
		t.Fatalf("chat: %+v", eb)
	}
	ev := waitEvent(t, sub, "integration chat ping")
	if ev.Level != protocol.LevelChat || ev.Operator != "dana" {
		t.Fatalf("chat event %+v", ev)
	}
}

// Two operators share one session: both see the transcript and live output,
// and one operator's detach does not silence the other's stream.
// Requires the lab's sshbox via MSF_SSH_TARGET.
func TestIntegrationTeamSharedSession(t *testing.T) {
	target := os.Getenv("MSF_SSH_TARGET")
	if target == "" {
		t.Skip("MSF_SSH_TARGET not set")
	}
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	e.OperatorJoin("dana")
	e.OperatorJoin("mile")

	_, eb := e.Exec(context.Background(), "dana", protocol.MethodModuleExecute, json.RawMessage(`{
		"type":"auxiliary","name":"scanner/ssh/ssh_login",
		"options":{"RHOSTS":"`+target+`","RPORT":2222,"USERNAME":"msfadmin","PASSWORD":"msfadmin","STOP_ON_SUCCESS":true}
	}`))
	if eb != nil {
		t.Fatalf("ssh_login launch: %+v", eb)
	}
	var sid string
	deadline := time.After(90 * time.Second)
	for sid == "" {
		for s := range e.State().Sessions {
			sid = s
		}
		if sid != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ssh_login never opened a session")
		case <-time.After(200 * time.Millisecond):
		}
	}

	// both operators open the same session; mile replays dana's transcript
	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodSessionAttach,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("dana attach: %+v", eb)
	}
	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodSessionWrite,
		json.RawMessage(`{"sid":"`+sid+`","data":"echo team-shared-one\n"}`)); eb != nil {
		t.Fatalf("dana write: %+v", eb)
	}
	waitOutput := func(op, needle string) {
		dl := time.After(30 * time.Second)
		for {
			if v := e.InteractFor(op); v != nil && strings.Contains(v.Output, needle) {
				return
			}
			select {
			case <-dl:
				t.Fatalf("%s never saw %q; transcript: %+v", op, needle, e.InteractFor(op))
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	waitOutput("dana", "team-shared-one")
	if _, eb := e.Exec(context.Background(), "mile", protocol.MethodSessionAttach,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("mile attach: %+v", eb)
	}
	waitOutput("mile", "team-shared-one") // shared transcript replay

	// dana closes her tab; mile's stream stays live
	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodSessionDetach,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("dana detach: %+v", eb)
	}
	if _, eb := e.Exec(context.Background(), "mile", protocol.MethodSessionWrite,
		json.RawMessage(`{"sid":"`+sid+`","data":"echo team-shared-two\n"}`)); eb != nil {
		t.Fatalf("mile write after dana detach: %+v", eb)
	}
	waitOutput("mile", "team-shared-two")

	// the last detach ends the transcript and the write guard returns
	if _, eb := e.Exec(context.Background(), "mile", protocol.MethodSessionDetach,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("mile detach: %+v", eb)
	}
	_, eb = e.Exec(context.Background(), "mile", protocol.MethodSessionWrite,
		json.RawMessage(`{"sid":"`+sid+`","data":"echo must-not-run\n"}`))
	if eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("write after last detach: got %+v, want busy", eb)
	}
	if _, eb := e.Exec(context.Background(), "dana", protocol.MethodSessionStop,
		json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("stop session: %+v", eb)
	}
}

// Requires a reachable lab target through MSF_SCAN_TARGET (for example the
// vulnbox IP printed by scripts/msf/up.sh --with-vulnbox).
func TestIntegrationServiceScanFingerprints(t *testing.T) {
	target := os.Getenv("MSF_SCAN_TARGET")
	if target == "" {
		t.Skip("MSF_SCAN_TARGET not set")
	}
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	_, eb := e.Exec(context.Background(), "integration-op", protocol.MethodServicesScan,
		json.RawMessage(`{"target":"`+target+`","ports":"80"}`))
	if eb != nil {
		t.Fatalf("services.scan: %+v", eb)
	}
	deadline := time.After(2 * time.Minute)
	finished := false
	for !finished {
		select {
		case m := <-sub.C():
			ev, ok := m.(protocol.EventMsg)
			if !ok || !strings.Contains(ev.Text, "service scan") {
				continue
			}
			if strings.Contains(ev.Text, "failed") {
				t.Fatalf("scan event: %s", ev.Text)
			}
			finished = strings.Contains(ev.Text, "finished")
		case <-deadline:
			for _, ev := range e.State().Events {
				t.Logf("event: %s", ev.Text)
			}
			t.Fatal("service scan did not finish within two minutes")
		}
	}

	var found *protocol.ServiceState
	for _, s := range e.State().Services {
		if s != nil && s.Host == target && s.Port == 80 && s.State == "open" {
			found = s
		}
	}
	if found == nil {
		t.Fatalf("open port 80 missing from %d services", len(e.State().Services))
	}
	if !strings.Contains(strings.ToLower(found.Info), "apache") && !strings.Contains(strings.ToLower(found.Name), "http") {
		t.Fatalf("service was not fingerprinted: %+v", found)
	}
}
