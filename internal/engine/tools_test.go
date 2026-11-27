package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
		return map[string]interface{}{"data": data, "prompt": "msf > ", "busy": false}, nil
	})
	return writes
}

func toolsEngine(t *testing.T, fake *fakeRPC) *Engine {
	t.Helper()
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	e.utilityPoll = time.Millisecond
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

// payloadExecuteFake answers module.execute for a payload module and records
// the arguments, so tests can assert what reached the daemon.
func payloadExecuteFake(fake *fakeRPC, artifact string) *map[string]interface{} {
	var opts map[string]interface{}
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		if args[0].(string) != "payload" {
			return nil, fmt.Errorf("module.execute type %v, want payload", args[0])
		}
		opts = args[2].(map[string]interface{})
		return map[string]interface{}{"payload": artifact}, nil
	})
	return &opts
}

func TestPayloadGenerateReturnsArtifactBytes(t *testing.T) {
	fake := stdFake()
	opts := payloadExecuteFake(fake, "SEVMTE8=")
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
	if payload.Filename != "reverse_tcp.b64" || payload.Size != len("SEVMTE8=") {
		t.Fatalf("payload %+v", payload)
	}
	if payload.Text != "SEVMTE8=" {
		t.Fatalf("text format must carry the raw text: %+v", payload)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.DataBase64)
	if err != nil || string(decoded) != "SEVMTE8=" {
		t.Fatalf("dataBase64 %q decodes to %q (%v)", payload.DataBase64, decoded, err)
	}
	if payload.SavedPath != "" {
		t.Fatalf("RPC generation must not claim a host path: %+v", payload)
	}
	if (*opts)["Format"] != "base64" || (*opts)["LHOST"] != "127.0.0.1" {
		t.Fatalf("options reaching module.execute: %v", *opts)
	}
	if _, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"x","format":"nope"}`)); eb == nil {
		t.Fatal("unknown format must be rejected")
	}
}

// Binary formats carry raw bytes with NULs through the RPC string; the
// base64 envelope must round-trip them exactly and no text preview is shown.
func TestPayloadGenerateBinaryFormatRoundTrips(t *testing.T) {
	fake := stdFake()
	elf := "\x7fELF\x02\x01\x01\x00"
	payloadExecuteFake(fake, elf)
	e := toolsEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"exe","options":{"LHOST":"127.0.0.1","LPORT":4444}}`))
	if eb != nil {
		t.Fatalf("payload.generate: %+v", eb)
	}
	var payload protocol.PayloadGeneratePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.DataBase64)
	if err != nil || string(decoded) != elf {
		t.Fatalf("binary round-trip: %q (%v)", decoded, err)
	}
	if payload.Text != "" || payload.Filename != "reverse_tcp.exe" || payload.Size != len(elf) {
		t.Fatalf("payload %+v", payload)
	}
}

func TestPayloadGenerateEmptyArtifactFails(t *testing.T) {
	fake := stdFake()
	payloadExecuteFake(fake, "")
	e := toolsEngine(t, fake)
	_, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"exe"}`))
	if eb == nil || eb.Code != protocol.CodeInternal {
		t.Fatalf("empty artifact: %+v", eb)
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

// Option values reach module.execute as typed: RPC carries them in a map,
// so spaces and quotes need no console quoting, and JSON numbers normalize
// to integers the msf option validator accepts.
func TestPayloadGeneratePassesOptionsVerbatim(t *testing.T) {
	fake := stdFake()
	opts := payloadExecuteFake(fake, "x")
	e := toolsEngine(t, fake)
	if _, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","format":"base64",
		  "options":{"HttpUserAgent":"Mozilla 5.0 o'brien","LPORT":4444,"PrependSleep":false},
		  "encoder":"x86/shikata_ga_nai","iterations":3}`)); eb != nil {
		t.Fatalf("payload.generate: %+v", eb)
	}
	got := *opts
	if got["HttpUserAgent"] != "Mozilla 5.0 o'brien" {
		t.Fatalf("HttpUserAgent %v", got["HttpUserAgent"])
	}
	if got["LPORT"] != int64(4444) || got["PrependSleep"] != false {
		t.Fatalf("LPORT %v (%T) PrependSleep %v", got["LPORT"], got["LPORT"], got["PrependSleep"])
	}
	if got["Encoder"] != "x86/shikata_ga_nai" || got["Iterations"] != 3 {
		t.Fatalf("encoder options %v", got)
	}
}

// An option name is an identifier; a newline in one would smuggle a second
// option into the module.execute call. Rejected names must not reach the RPC.
func TestPayloadGenerateRejectsOptionNameInjection(t *testing.T) {
	fake := stdFake()
	var calls int32
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return map[string]interface{}{"payload": "x"}, nil
	})
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
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("rejected option names still reached module.execute (%d calls)", calls)
	}
}

// A value is not a name: quoting keeps its spaces and quotes in one argv
// token, but the console still reads input line by line. An embedded newline
// must be rejected before any of the command reaches the console, or the
// second line would run as a command of its own. Payload options travel by
// RPC instead, and a newline there is refused before module.execute.
func TestUtilityConsoleRejectsLineBreakInjection(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake, "banner\nmsf > ")
	var payCalls int32
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		atomic.AddInt32(&payCalls, 1)
		return map[string]interface{}{"payload": "x"}, nil
	})
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
		if strings.Contains(sent, "creds add") {
			t.Fatalf("rejected value still reached the console: %v", writes.all())
		}
	}
	if atomic.LoadInt32(&payCalls) != 0 {
		t.Fatal("rejected payload value still reached module.execute")
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
	if eb := e.attach("", "1"); eb != nil {
		t.Fatal(eb)
	}
	e.sessionOutput(mon, gomsf.Event{SessionID: "1", Data: "one\n"})
	if eb := e.attach("", "2"); eb != nil {
		t.Fatal(eb)
	}
	e.sessionOutput(mon, gomsf.Event{SessionID: "2", Data: "two\n"})
	if eb := e.attach("", "1"); eb != nil {
		t.Fatal(eb)
	}
	st := e.State()
	if st.Interact == nil || st.Interact.SID != "1" || !strings.Contains(st.Interact.Output, "one") {
		t.Fatalf("interact after switching back: %+v", st.Interact)
	}
	// closing the other tab must not touch the active transcript
	e.detach("", "2")
	st = e.State()
	if st.Interact == nil || st.Interact.SID != "1" {
		t.Fatalf("interact after detaching other tab: %+v", st.Interact)
	}
}

// A loot row past the daemon's 100-row unpaged cap must still resolve. The
// request filters by type only so the target can sit beyond the first page;
// paging must walk to it.
func TestLootDownloadPagesPastTheDaemonCap(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.DbLoots, func(args ...interface{}) (interface{}, error) {
		opts := args[0].(map[string]interface{})
		limit, offset := 100, 0
		if v, ok := opts["limit"].(int); ok {
			limit = v
		}
		if v, ok := opts["offset"].(int); ok {
			offset = v
		}
		rows := []interface{}{}
		for i := offset; i < 205 && i < offset+limit; i++ {
			host := "10.0.0.9"
			if i == 204 {
				host = "10.0.0.5"
			}
			rows = append(rows, map[string]interface{}{
				"host": host, "ltype": "host.file", "name": "",
				"data": fmt.Sprintf("/msf/loot/%03d", i),
			})
		}
		return map[string]interface{}{"loots": rows}, nil
	})
	e := connectedEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", "loot.download", json.RawMessage(
		`{"host":"10.0.0.5","type":"host.file"}`))
	if eb != nil {
		t.Fatalf("loot.download: %+v", eb)
	}
	var payload protocol.LootPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data != "/msf/loot/204" {
		t.Fatalf("payload %+v", payload)
	}
}
