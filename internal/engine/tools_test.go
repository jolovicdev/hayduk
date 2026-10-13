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

// scriptedConsole answers utility-console reads from a queue, one chunk per
// read, and records every line written to console 7.
func scriptedConsole(fake *fakeRPC, chunks ...string) *[]string {
	var mu sync.Mutex
	queue := append([]string(nil), chunks...)
	writes := &[]string{}
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "7"}, nil
	})
	fake.set(gomsf.ConsoleWrite, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		*writes = append(*writes, args[1].(string))
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
		"[*] Writing 1060 bytes to /tmp/hayduk-reverse_tcp.base64...\nmsf payload(linux/x64/meterpreter/reverse_tcp) > ",
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
	if payload.SavedPath != "/tmp/hayduk-reverse_tcp.base64" || payload.Filename == "" {
		t.Fatalf("payload %+v writes %v", payload, *writes)
	}
	joined := strings.Join(*writes, "")
	if !strings.Contains(joined, "use payload/linux/x64/meterpreter/reverse_tcp") ||
		!strings.Contains(joined, "generate -f base64 -o /tmp/hayduk-reverse_tcp.base64") {
		t.Fatalf("console writes %v", *writes)
	}
	if _, eb := e.Exec(context.Background(), "", "payload.generate", json.RawMessage(
		`{"payload":"x","format":"nope"}`)); eb == nil {
		t.Fatal("unknown format must be rejected")
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
	joined := strings.Join(*writes, "")
	for _, want := range []string{"creds add", "user:carol", "creds -d 10.0.0.5", "services -d -p 22 10.0.0.5"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("console writes missing %q: %v", want, *writes)
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
