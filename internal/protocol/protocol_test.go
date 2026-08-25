package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHelloGolden(t *testing.T) {
	got := marshal(t, NewHello("1.2.3", false))
	want := `{"type":"hello","proto":1,"version":"1.2.3","team":false}`
	if got != want {
		t.Fatalf("hello: got %s want %s", got, want)
	}
}

func TestSnapshotGolden(t *testing.T) {
	got := marshal(t, NewSnapshot(CampaignState{
		Connection: ConnectionState{Status: "connected", Workspace: "default"},
		Sessions:   map[string]*SessionState{"1": {ID: "1", Type: "meterpreter", TargetHost: "10.0.0.1"}},
	}))
	want := `{"type":"snapshot","state":{"connection":{"status":"connected","host":"","port":0,"ssl":false,"username":"","msfVersion":"","workspace":"default"},"hosts":null,"services":null,"sessions":{"1":{"id":"1","type":"meterpreter","targetHost":"10.0.0.1"}},"jobs":null,"routes":null,"creds":null,"loot":null,"modules":null,"moduleRanks":null,"console":null,"interact":null,"events":null,"operators":null}}`
	if got != want {
		t.Fatalf("snapshot:\n got %s\nwant %s", got, want)
	}
}

func TestResourceUpdateGolden(t *testing.T) {
	got := marshal(t, SessionsUpdate(map[string]*SessionState{"2": {ID: "2", Type: "shell"}}))
	want := `{"type":"resource","resource":"sessions","sessions":{"2":{"id":"2","type":"shell"}}}`
	if got != want {
		t.Fatalf("resource: got %s want %s", got, want)
	}
}

func TestEventGolden(t *testing.T) {
	ts := time.Date(2026, 8, 25, 12, 30, 5, 0, time.UTC)
	got := marshal(t, EventMsg{Type: KindEvent, Seq: 7, Level: LevelSuccess, Text: "session 1 opened", Time: ts})
	want := `{"type":"event","seq":7,"time":"2026-08-25T12:30:05Z","level":"success","text":"session 1 opened"}`
	if got != want {
		t.Fatalf("event: got %s want %s", got, want)
	}
}

func TestServerMessageFromEventCarriesTime(t *testing.T) {
	ts := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	msg := ServerMessageFromEvent(&EventEntry{Seq: 3, Time: ts, Level: LevelWarn, Text: "rpc error"})
	if !msg.Time.Equal(ts) {
		t.Fatalf("streamed event lost its server timestamp: %+v", msg)
	}
}

func TestResponseGolden(t *testing.T) {
	ok := marshal(t, OKResponse(3, json.RawMessage(`["a","b"]`)))
	wantOK := `{"type":"response","id":3,"ok":true,"data":["a","b"]}`
	if ok != wantOK {
		t.Fatalf("ok: got %s want %s", ok, wantOK)
	}
	errResp := marshal(t, ErrorResponse(4, "unknown_method", "no such method"))
	wantErr := `{"type":"response","id":4,"ok":false,"error":{"code":"unknown_method","message":"no such method"}}`
	if errResp != wantErr {
		t.Fatalf("err: got %s want %s", errResp, wantErr)
	}
}

func TestClientMessageRoundtrip(t *testing.T) {
	in := `{"type":"command","id":9,"method":"console.write","params":{"command":"version\n"}}`
	var m ClientMessage
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatal(err)
	}
	if m.Method != "console.write" || m.ID != 9 {
		t.Fatalf("parsed %+v", m)
	}
	var p ConsoleWriteParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.Command != "version\n" {
		t.Fatalf("params %+v", p)
	}
}
