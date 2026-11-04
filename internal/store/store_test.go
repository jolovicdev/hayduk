package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testEvent(seq int64, text string) *protocol.EventEntry {
	return &protocol.EventEntry{
		Seq: seq, Time: time.Date(2026, 11, 4, 12, 0, int(seq), 0, time.UTC),
		Level: protocol.LevelInfo, Text: text, Operator: "dana", Workspace: "default",
	}
}

func TestEventLogRoundtripAndSequence(t *testing.T) {
	st := openTestStore(t)
	for i := int64(1); i <= 3; i++ {
		st.AppendEvent(testEvent(i, fmt.Sprintf("event %d", i)))
	}
	if err := st.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	events, err := st.LoadEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Seq != 1 || events[2].Text != "event 3" {
		t.Fatalf("events %+v", events)
	}
	if events[2].Operator != "dana" || events[2].Workspace != "default" {
		t.Fatalf("attribution lost: %+v", events[2])
	}
}

func TestEventLogLimitKeepsNewest(t *testing.T) {
	st := openTestStore(t)
	for i := int64(1); i <= 5; i++ {
		st.AppendEvent(testEvent(i, fmt.Sprintf("event %d", i)))
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	events, err := st.LoadEvents(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Seq != 4 || events[1].Seq != 5 {
		t.Fatalf("newest events %+v", events)
	}
}

func TestEventLogSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, eventsFileName)
	content := `{"seq":1,"level":"info","text":"good one"}
not json
{"seq":2,"level":"info","text":"good two"}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	events, err := st.LoadEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Text != "good one" || events[1].Text != "good two" {
		t.Fatalf("events %+v", events)
	}
}

func TestEventLogRotates(t *testing.T) {
	old := maxEventFileSize
	maxEventFileSize = 120
	t.Cleanup(func() { maxEventFileSize = old })

	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := int64(1); i <= 3; i++ {
		st.AppendEvent(testEvent(i, fmt.Sprintf("rotation %d", i)))
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, rotatedEventsName)); err != nil {
		t.Fatalf("no rotated log: %v", err)
	}
	events, err := st.LoadEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("rotation lost events: %+v", events)
	}
}

func TestSnapshotsRoundtrip(t *testing.T) {
	st := openTestStore(t)
	if err := st.SaveRanks(map[string]string{"exploit/windows/smb/ms17_010": "great"}); err != nil {
		t.Fatal(err)
	}
	if ranks := st.LoadRanks(); ranks["exploit/windows/smb/ms17_010"] != "great" {
		t.Fatalf("ranks %+v", ranks)
	}

	listeners := []*protocol.ListenerState{{
		JobID: "3", Payload: "windows/x64/meterpreter/reverse_tcp",
		Lhost: "10.0.0.99", Lport: 4444, Operator: "dana",
	}}
	if err := st.SaveListeners(listeners); err != nil {
		t.Fatal(err)
	}
	if got := st.LoadListeners(); len(got) != 1 || got[0].JobID != "3" || got[0].Lport != 4444 {
		t.Fatalf("listeners %+v", got)
	}
}

func TestProfilesUpsertNewestFirstAndCapped(t *testing.T) {
	st := openTestStore(t)
	for i := 0; i < maxProfiles+3; i++ {
		_, err := st.SaveProfile(protocol.ProfilePayload{
			Host: fmt.Sprintf("10.0.0.%d", i), Port: 55553, Username: "msf",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	profiles := st.ListProfiles()
	if len(profiles) != maxProfiles {
		t.Fatalf("profiles %d, want cap %d", len(profiles), maxProfiles)
	}
	if profiles[0].Host != fmt.Sprintf("10.0.0.%d", maxProfiles+2) {
		t.Fatalf("newest profile not first: %+v", profiles[0])
	}

	// saving an existing endpoint moves it to the front without duplicating
	_, err := st.SaveProfile(protocol.ProfilePayload{Host: profiles[1].Host, Port: 55553, Username: "msf"})
	if err != nil {
		t.Fatal(err)
	}
	got := st.ListProfiles()
	if len(got) != maxProfiles || got[0].Host != profiles[1].Host {
		t.Fatalf("upsert result %+v", got)
	}
}

func TestCloseIsIdempotentAndStopsAppends(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	st.AppendEvent(testEvent(1, "after close")) // must not panic
}

func TestOpenRejectsEmptyDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("empty data dir accepted")
	}
}
