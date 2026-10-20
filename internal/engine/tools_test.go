package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestJobStopCommand(t *testing.T) {
	fake := stdFake()
	var stopped []string
	fake.set(gomsf.JobStop, func(args ...interface{}) (interface{}, error) {
		stopped = append(stopped, args[0].(string))
		return map[string]interface{}{"result": "success"}, nil
	})
	e := connectedEngine(t, fake)
	if _, eb := e.Exec(context.Background(), "", "job.stop", json.RawMessage(`{"id":"3"}`)); eb != nil {
		t.Fatalf("job.stop: %+v", eb)
	}
	if len(stopped) != 1 || stopped[0] != "3" {
		t.Fatalf("stopped %v", stopped)
	}
}

func TestSessionRunMeterpreter(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"4": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
			"5": map[string]interface{}{"type": "shell", "target_host": "10.0.0.6"},
		}, nil
	})
	var ran []string
	fake.set(gomsf.SessionMeterpreterRunSingle, func(args ...interface{}) (interface{}, error) {
		ran = append(ran, args[1].(string))
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.SessionMeterpreterRead, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"data": "Computer: WS1\nOS: Windows 11\n"}, nil
	})
	e := connectedEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", "session.run", json.RawMessage(`{"sid":"4","command":"sysinfo"}`))
	if eb != nil {
		t.Fatalf("session.run: %+v", eb)
	}
	var payload protocol.SessionRunPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Output, "Windows 11") || len(ran) != 1 || ran[0] != "sysinfo" {
		t.Fatalf("output %q ran %v", payload.Output, ran)
	}
	// shells are rejected: their tools are the raw Interact console
	if _, eb := e.Exec(context.Background(), "", "session.run", json.RawMessage(`{"sid":"5","command":"id"}`)); eb == nil {
		t.Fatal("shell session.run must be rejected")
	}
}

// consoleWrites records the lines written to any console the fake saw. The
// engine's route loop drives its own console through the same fake and
// writes `route` to it at connect, so a test that wants to know whether a
// command reached the console asks about that command rather than counting
// lines. The lock is what keeps the recorder itself race-free.
type consoleWrites struct {
	mu    sync.Mutex
	lines []string
}

func (c *consoleWrites) add(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
}

func (c *consoleWrites) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func (c *consoleWrites) joined() string {
	return strings.Join(c.all(), "")
}

func (c *consoleWrites) lastOutputPath() string {
	path := ""
	for _, line := range c.all() {
		if _, after, ok := strings.Cut(line, "-o "); ok {
			path = strings.TrimSpace(after)
		}
	}
	return path
}

// scriptedConsole answers utility-console reads from a queue, one chunk per
// read, and records every line written to the console it hands out.
func scriptedConsole(fake *fakeRPC, chunks ...string) *consoleWrites {
	// the route loop reads its own console through this fake too, so the
	// scripted queue needs a lock of its own
	var mu sync.Mutex
	queue := append([]string(nil), chunks...)
	writes := &consoleWrites{}
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "7"}, nil
	})
	fake.set(gomsf.ConsoleWrite, func(args ...interface{}) (interface{}, error) {
		writes.add(args[1].(string))
		return map[string]interface{}{"wrote": true}, nil
	})
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		data := ""
		if len(queue) > 0 {
			data = queue[0]
			queue = queue[1:]
		}
		// msf echoes back the path it was handed, so a scripted chunk
		// containing %OUT% stands in for whatever -o the last write named.
		// Such a chunk waits its turn: the fake always reports idle, so a
		// drain reads several times before the command it answers is even
		// written.
		if strings.Contains(data, "%OUT%") {
			if out := writes.lastOutputPath(); out != "" {
				data = strings.ReplaceAll(data, "%OUT%", out)
			} else {
				queue = append([]string{data}, queue...)
				data = ""
			}
		}
		return map[string]interface{}{"data": data, "prompt": "msf > ", "busy": false}, nil
	})
	return writes
}

func toolsEngine(t *testing.T, fake *fakeRPC) *Engine {
	t.Helper()
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

func TestPayloadGenerateSavesOnHost(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake,
		"banner\nmsf > ",
		"",
		"LHOST => 127.0.0.1\n",
		"[*] Writing 1060 bytes to %OUT%...\nmsf payload(linux/x64/meterpreter/reverse_tcp) > ",
	)
	e := toolsEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"base64","options":{"LHOST":"127.0.0.1"}}`))
	if eb != nil {
		t.Fatalf("payload.generate: %+v", eb)
	}
	var payload protocol.PayloadGeneratePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	// the file lands in a world-writable directory, so the name carries a
	// random component: a predictable one can be pre-created as a symlink
	// and `generate -o` would write through it
	if !strings.HasPrefix(payload.SavedPath, "/tmp/hayduk-reverse_tcp-") ||
		!strings.HasSuffix(payload.SavedPath, ".base64") {
		t.Fatalf("payload %+v writes %v", payload, writes.all())
	}
	if payload.Filename != strings.TrimPrefix(payload.SavedPath, "/tmp/") {
		t.Fatalf("filename %q does not match saved path %q", payload.Filename, payload.SavedPath)
	}
	joined := writes.joined()
	if !strings.Contains(joined, "use payload/linux/x64/meterpreter/reverse_tcp") ||
		!strings.Contains(joined, "generate -f base64 -o "+payload.SavedPath) {
		t.Fatalf("console writes %v", writes.all())
	}
	if _, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"x","format":"nope"}`)); eb == nil {
		t.Fatal("unknown format must be rejected")
	}
}

// Two generations must not land on the same path, or the second overwrites
// the first at a name another local user could have claimed.
func TestPayloadGenerateNamesDifferPerRun(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		path := payloadOutputPath("reverse_tcp", "bin")
		if seen[path] {
			t.Fatalf("payload path %q repeated across runs", path)
		}
		seen[path] = true
	}
}

func TestDBWriteCommands(t *testing.T) {
	fake := stdFake()
	var reported, deleted, imported []map[string]interface{}
	fake.set(gomsf.DbReportHost, func(args ...interface{}) (interface{}, error) {
		reported = append(reported, args[0].(map[string]interface{}))
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.DbDelHost, func(args ...interface{}) (interface{}, error) {
		deleted = append(deleted, args[0].(map[string]interface{}))
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.DbImportData, func(args ...interface{}) (interface{}, error) {
		imported = append(imported, args[0].(map[string]interface{}))
		return map[string]interface{}{"result": "success"}, nil
	})
	e := connectedEngine(t, fake)
	exec := func(method, params string) {
		t.Helper()
		if _, eb := e.Exec(context.Background(), "", method, json.RawMessage(params)); eb != nil {
			t.Fatalf("%s: %+v", method, eb)
		}
	}
	exec("db.host_add", `{"address":"10.9.9.9","name":"ws9","osName":"Linux"}`)
	exec("db.host_note", `{"address":"10.9.9.9","comments":"patch tuesday"}`)
	exec("db.import", `{"data":"<nmaprun/>"}`)
	exec("db.host_delete", `{"address":"10.9.9.9"}`)
	if len(reported) != 2 || reported[0]["host"] != "10.9.9.9" || reported[1]["comments"] != "patch tuesday" {
		t.Fatalf("reported %v", reported)
	}
	if len(imported) != 1 || imported[0]["data"] != "<nmaprun/>" {
		t.Fatalf("imported %v", imported)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted %v", deleted)
	}
}

func TestCredAndServiceConsoleCommands(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake,
		"banner\nmsf > ", "ok\nmsf > ",
		"banner\nmsf > ", "Deleted 1 creds\nmsf > ",
		"banner\nmsf > ", "Deleted 1 services\nmsf > ",
	)
	e := toolsEngine(t, fake)
	exec := func(method, params string) {
		t.Helper()
		if _, eb := e.Exec(context.Background(), "", method, json.RawMessage(params)); eb != nil {
			t.Fatalf("%s: %+v", method, eb)
		}
	}
	exec("db.cred_add", `{"host":"10.0.0.5","port":22,"service":"ssh","user":"carol","pass":"secret123"}`)
	exec("db.cred_delete", `{"host":"10.0.0.5","port":22,"user":"carol"}`)
	exec("db.service_delete", `{"host":"10.0.0.5","port":22,"proto":"tcp"}`)
	joined := writes.joined()
	for _, want := range []string{"creds add", "user:carol", "creds -d 10.0.0.5", "services -d -p 22 10.0.0.5"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("console writes missing %q: %v", want, writes.all())
		}
	}
}

// `services -d` cannot select one protocol: Metasploit's services command
// drops -r before the delete search, so a tcp/udp pair on one port loses
// both rows when the operator deletes one of them. The command has to say
// so instead of deleting the row they did not click.
func TestServiceDeleteRefusesAcrossProtocols(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake, "banner\nmsf > ", "Deleted 1 services\nmsf > ")
	fake.set(gomsf.DbServices, func(args ...interface{}) (interface{}, error) {
		rows := []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "port": 53, "proto": "udp", "name": "domain"},
			map[string]interface{}{"host": "10.0.0.5", "port": 53, "proto": "tcp", "name": "domain"},
			map[string]interface{}{"host": "10.0.0.5", "port": 22, "proto": "tcp", "name": "ssh"},
		}
		return map[string]interface{}{"services": rows}, nil
	})
	e := toolsEngine(t, fake)
	if err := e.refreshDB(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, eb := e.Exec(context.Background(), "", "db.service_delete", json.RawMessage(
		`{"host":"10.0.0.5","port":53,"proto":"tcp"}`))
	if eb == nil {
		t.Fatal("deleting the tcp row must not take the udp row with it")
	}
	if !strings.Contains(eb.Message, "tcp and udp") {
		t.Fatalf("error %q does not name both protocols", eb.Message)
	}
	if strings.Contains(writes.joined(), "services -d") {
		t.Fatalf("refused delete still reached the console: %v", writes.all())
	}
	// one protocol on the port is unambiguous, so it still deletes
	if _, eb := e.Exec(context.Background(), "", "db.service_delete", json.RawMessage(
		`{"host":"10.0.0.5","port":22,"proto":"tcp"}`)); eb != nil {
		t.Fatalf("unambiguous delete rejected: %+v", eb)
	}
}

// An option value with a space - a User-Agent, a custom command - has to
// reach msfconsole as one token, and one carrying a newline must not break
// out into a console line of its own.
func TestPayloadGenerateQuotesOptionValues(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake,
		"banner\nmsf > ", "", "", "",
		"[*] Writing 1060 bytes to %OUT%...\nmsf > ",
	)
	e := toolsEngine(t, fake)
	if _, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"base64",
		  "options":{"HttpUserAgent":"Mozilla 5.0 o'brien","LPORT":4444,"PrependSleep":false}}`)); eb != nil {
		t.Fatalf("payload.generate: %+v", eb)
	}
	joined := writes.joined()
	for _, want := range []string{
		`set HttpUserAgent 'Mozilla 5.0 o'\''brien'`,
		"set LPORT 4444",
		"set PrependSleep false",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("console writes missing %q: %v", want, writes.all())
		}
	}
}

// An option name is an identifier; a newline in one would smuggle a second
// console command into the utility console's script.
func TestPayloadGenerateRejectsOptionNameInjection(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake, "banner\nmsf > ")
	e := toolsEngine(t, fake)
	for _, name := range []string{"FOO\nset LHOST 1.2.3.4", "set X", "A B", ""} {
		raw, _ := json.Marshal(map[string]interface{}{
			"payload": "linux/x64/meterpreter/reverse_tcp", "format": "base64",
			"options": map[string]interface{}{name: "x"},
		})
		if _, eb := e.Exec(context.Background(), "", "payload.generate", raw); eb == nil {
			t.Fatalf("option name %q was accepted", name)
		}
	}
	for _, sent := range writes.all() {
		if strings.Contains(sent, "set ") || strings.Contains(sent, "generate ") ||
			strings.Contains(sent, "use payload/") {
			t.Fatalf("rejected option still reached the console: %v", writes.all())
		}
	}
}

// A value is not a name: quoting keeps its spaces and quotes in one argv
// token, but the console still reads input line by line. An embedded newline
// must be rejected before any of the command reaches the console, or the
// second line would run as a command of its own.
func TestUtilityConsoleRejectsLineBreakInjection(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake, "banner\nmsf > ")
	e := toolsEngine(t, fake)

	_, eb := e.Exec(context.Background(), "", "db.cred_add", json.RawMessage(
		`{"host":"10.0.0.5","user":"carol","pass":"x\ncreds add address:10.0.0.9 user:evil pass:evil"}`))
	if eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("credential with a line break: %+v", eb)
	}
	_, eb = e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"base64",
		  "options":{"HttpUserAgent":"x\nuse exploit/multi/handler"}}`))
	if eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("payload option with a line break: %+v", eb)
	}
	for _, sent := range writes.all() {
		if strings.Contains(sent, "creds add") || strings.Contains(sent, "use payload/") {
			t.Fatalf("rejected value still reached the console: %v", writes.all())
		}
	}
}

// msfconsole splits a console line with shellsplitex, a copy of Ruby's
// Shellwords.split: outside quotes a backslash escapes the next character,
// and a single-quoted run protects everything up to the next quote. A
// value holding a quote must therefore be quoted with that quote escaped
// the POSIX way - close, backslash-escape, reopen - because deleting it
// writes a different credential than the operator typed. The expectations
// here are what Ruby 3.3's Shellwords.shellsplit yields for the msfconsole
// line built from each emitted value.
func TestConsoleArgQuotesForMsfconsole(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"carol", "carol"},
		{"pa ss", "'pa ss'"},
		{"P@ss w0rd!", "'P@ss w0rd!'"},
		// an embedded quote would otherwise close the quoted run early and
		// leave the rest to be split into separate tokens
		{"pa'ss", `'pa'\''ss'`},
		{"admin' OR 1=1--", `'admin'\'' OR 1=1--'`},
		{"trailing'", `'trailing'\'''`},
		// a bare double quote opens a quoted run that never closes
		{`pa"ss`, `'pa"ss'`},
		// a bare backslash escapes the character after it, eating one
		{`pa\ss`, `'pa\ss'`},
	} {
		if got := consoleArg(tc.in); got != tc.want {
			t.Errorf("consoleArg(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLootDownloadReturnsStoredData(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.DbLoots, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"loots": []interface{}{
			map[string]interface{}{"host": "10.0.0.5", "ltype": "host.file", "name": "a.txt", "data": "/msf/loot/a.txt", "info": "i"},
		}}, nil
	})
	e := connectedEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", "loot.download", json.RawMessage(
		`{"host":"10.0.0.5","type":"host.file","name":"a.txt"}`))
	if eb != nil {
		t.Fatalf("loot.download: %+v", eb)
	}
	var payload protocol.LootPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data != "/msf/loot/a.txt" {
		t.Fatalf("payload %+v", payload)
	}
}

func TestHandlerIsNotTrackedAsExploitRun(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"PAYLOAD": map[string]interface{}{"type": "string", "required": false, "desc": "p"},
		}, nil
	})
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"job_id": 0, "uuid": "h1"}, nil
	})
	e := connectedEngine(t, fake)
	if _, eb := e.Exec(context.Background(), "", "module.execute", json.RawMessage(
		`{"type":"exploit","name":"multi/handler","options":{"PAYLOAD":"linux/x64/meterpreter/reverse_tcp"}}`)); eb != nil {
		t.Fatalf("handler execute: %+v", eb)
	}
	e.mu.Lock()
	runs := len(e.exploitRuns)
	e.mu.Unlock()
	if runs != 0 {
		t.Fatalf("listener tracked as exploit run: %d runs", runs)
	}
}

func TestMultiAttachKeepsTranscripts(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"1": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
			"2": map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.6"},
		}, nil
	})
	e := connectedEngine(t, fake)
	waitFor(t, func() bool { return len(e.State().Sessions) == 2 })
	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	if eb := e.attach("1"); eb != nil {
		t.Fatal(eb)
	}
	e.sessionOutput(mon, gomsf.Event{SessionID: "1", Data: "one\n"})
	if eb := e.attach("2"); eb != nil {
		t.Fatal(eb)
	}
	e.sessionOutput(mon, gomsf.Event{SessionID: "2", Data: "two\n"})
	if eb := e.attach("1"); eb != nil {
		t.Fatal(eb)
	}
	st := e.State()
	if st.Interact == nil || st.Interact.SID != "1" || !strings.Contains(st.Interact.Output, "one") {
		t.Fatalf("interact after switching back: %+v", st.Interact)
	}
	// closing the other tab must not touch the active transcript
	e.detach("2")
	st = e.State()
	if st.Interact == nil || st.Interact.SID != "1" {
		t.Fatalf("interact after detaching other tab: %+v", st.Interact)
	}
}
