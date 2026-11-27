package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestParseFsListing(t *testing.T) {
	linux := `Listing: /home/msfadmin
=======================

Mode              Size  Type  Last modified              Name
----              ----  ----  -------------              ----
100644/rw-r--r--  220   fil   2023-08-04 12:00:00 +0000  .bash_logout
040755/rwxr-xr-x  4096  dir   2023-08-04 12:00:00 +0000  .ssh
100644/rw-r--r--  1234  fil   2024-01-02 03:04:05 +0000  my file.txt
`
	entries, path, errText := parseFsListing(linux)
	if errText != "" || path != "/home/msfadmin" {
		t.Fatalf("path %q err %q", path, errText)
	}
	if len(entries) != 3 {
		t.Fatalf("entries %+v", entries)
	}
	if entries[1].Name != ".ssh" || entries[1].Kind != "dir" || entries[1].Size != 4096 {
		t.Fatalf("dir row %+v", entries[1])
	}
	// a name with spaces survives: everything after date/time/zone is the name
	if entries[2].Name != "my file.txt" || entries[2].Kind != "file" || entries[2].Modified != "2024-01-02 03:04:05 +0000" {
		t.Fatalf("spaced row %+v", entries[2])
	}

	windows := `Listing: C:\Users
=================

Mode              Size  Type  Last modified              Name
----              ----  ----  -------------              ----
040777/rwxrwxrwx  0     dir   2024-01-01 00:00:00 +0000  Public
100666/rw-rw-rw-  512   fil   2024-01-01 00:00:00 +0000  desktop.ini
`
	entries, path, errText = parseFsListing(windows)
	if errText != "" || path != `C:\Users` || len(entries) != 2 || entries[0].Kind != "dir" {
		t.Fatalf("windows: path %q err %q entries %+v", path, errText, entries)
	}

	entries, _, errText = parseFsListing("No entries exist in /empty\n")
	if errText != "" || len(entries) != 0 {
		t.Fatalf("empty dir: err %q entries %+v", errText, entries)
	}

	_, _, errText = parseFsListing("[-] The system cannot find the path specified.\n")
	if !strings.Contains(errText, "cannot find the path") {
		t.Fatalf("error listing: %q", errText)
	}
}

func TestResolveStagingModes(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1", "::1", "localhost", "LOCALHOST"} {
		if !msfHostIsLocal(host) {
			t.Errorf("host %q should be local", host)
		}
	}
	for _, host := range []string{"10.0.0.9", "msf.example.com", "192.168.1.10"} {
		if msfHostIsLocal(host) {
			t.Errorf("host %q should be remote", host)
		}
	}

	dir, auto, err := resolveStaging("", "127.0.0.1")
	if err != nil || !auto || dir == "" {
		t.Fatalf("auto staging: %q %v %v", dir, auto, err)
	}
	defer os.RemoveAll(dir)
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("staging dir perms: %v %v", info, err)
	}

	dir, auto, err = resolveStaging("", "10.0.0.9")
	if err != nil || auto || dir != "" {
		t.Fatalf("remote staging: %q %v %v", dir, auto, err)
	}

	explicit := t.TempDir()
	if err := os.Chmod(explicit, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, auto, err = resolveStaging(explicit, "10.0.0.9")
	if err != nil || auto || dir != explicit {
		t.Fatalf("explicit staging: %q %v %v", dir, auto, err)
	}

	shared := filepath.Join(t.TempDir(), "world")
	if err := os.MkdirAll(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveStaging(shared, ""); err == nil {
		t.Fatal("world-writable staging dir accepted")
	}
}

// meterpreterFake seeds one meterpreter session and routes tool reads by the
// command that was run, so fs_list can answer pwd and ls differently.
func meterpreterFake(fake *fakeRPC, sid string, reads func(cmd string) string) *[]string {
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			sid: map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
		}, nil
	})
	var mu sync.Mutex
	var cmds []string
	last := ""
	fake.set(gomsf.SessionMeterpreterRunSingle, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		last = args[1].(string)
		cmds = append(cmds, last)
		return map[string]interface{}{"result": "success"}, nil
	})
	fake.set(gomsf.SessionMeterpreterWrite, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		last = strings.TrimSuffix(args[1].(string), "\n")
		cmds = append(cmds, last)
		return map[string]interface{}{"wrote": true}, nil
	})
	fake.set(gomsf.SessionMeterpreterRead, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		cmd := last
		mu.Unlock()
		return map[string]interface{}{"data": reads(cmd)}, nil
	})
	fake.set(gomsf.SessionMeterpreterDirectorySeparator, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"separator": "/"}, nil
	})
	return &cmds
}

func TestSessionFsListReturnsRows(t *testing.T) {
	fake := stdFake()
	meterpreterFake(fake, "4", func(cmd string) string {
		if cmd == "pwd" {
			return "/home/msfadmin\n"
		}
		return "Listing: /home/msfadmin\n\nMode  Size  Type  Last modified  Name\n----  ----  ----  -------------  ----\n" +
			"040755/rwxr-xr-x  4096  dir  2023-08-04 12:00:00 +0000  docs\n"
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionFsList, json.RawMessage(`{"sid":"4"}`))
	if eb != nil {
		t.Fatalf("fs_list: %+v", eb)
	}
	var payload protocol.FsListPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path != "/home/msfadmin" || payload.Separator != "/" || len(payload.Entries) != 1 {
		t.Fatalf("payload %+v", payload)
	}
	if payload.Entries[0].Name != "docs" || payload.Entries[0].Kind != "dir" {
		t.Fatalf("entry %+v", payload.Entries[0])
	}
}

// transferFake answers RunWithOutput commands: onWrite returns one or more
// output chunks delivered across successive reads so tests can exercise
// multi-chunk progress lines before the terminal completion marker.
func transferFake(fake *fakeRPC, sid string, onWrite func(cmd string) []string) *[]string {
	var mu sync.Mutex
	var cmds []string
	var queue []string
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			sid: map[string]interface{}{"type": "meterpreter", "target_host": "10.0.0.5"},
		}, nil
	})
	fake.set(gomsf.SessionMeterpreterWrite, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		cmd := strings.TrimSuffix(args[1].(string), "\n")
		cmds = append(cmds, cmd)
		queue = append(queue, onWrite(cmd)...)
		return map[string]interface{}{"wrote": true}, nil
	})
	fake.set(gomsf.SessionMeterpreterRead, func(args ...interface{}) (interface{}, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(queue) == 0 {
			return map[string]interface{}{"data": ""}, nil
		}
		data := queue[0]
		queue = queue[1:]
		return map[string]interface{}{"data": data}, nil
	})
	return &cmds
}

func TestSessionFileDownloadStagesBytes(t *testing.T) {
	fake := stdFake()
	content := []byte("remote file bytes\x00\xff")
	var stageDir string
	cmds := transferFake(fake, "4", func(cmd string) []string {
		fields := strings.Fields(cmd)
		if len(fields) != 3 || fields[0] != "download" {
			return []string{"[-] unexpected command\n"}
		}
		stageDir = fields[2]
		// Mirror Metasploit's file.download: when dest is a directory whose
		// basename differs from src, MSF writes <dest>/<basename(src)> and
		// emits progress lines before the terminal Completed line.
		info, err := os.Stat(stageDir)
		if err != nil || !info.IsDir() {
			return []string{"[-] destination is not a directory\n"}
		}
		landed := filepath.Join(stageDir, filepath.Base(fields[1]))
		if err := os.WriteFile(landed, content, 0o600); err != nil {
			return []string{"[-] " + err.Error() + "\n"}
		}
		return []string{
			"[*] Downloading: " + fields[1] + " -> " + landed + "\n" +
				"[*] Downloaded 10.00 B of 19.00 B (52.63%): " + fields[1] + " -> " + landed + "\n",
			"[*] Completed  : " + fields[1] + " -> " + landed + "\n",
		}
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileDownload, json.RawMessage(
		`{"sid":"4","path":"/remote/dir/f.bin"}`))
	if eb != nil {
		t.Fatalf("file_download: %+v", eb)
	}
	var payload protocol.FileTransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.DataBase64)
	if err != nil || string(decoded) != string(content) {
		t.Fatalf("round-trip: %q (%v)", decoded, err)
	}
	if payload.Filename != "f.bin" || payload.Size != len(content) || payload.SavedPath != "" {
		t.Fatalf("payload %+v", payload)
	}
	if len(*cmds) != 1 || !strings.HasPrefix((*cmds)[0], "download /remote/dir/f.bin ") {
		t.Fatalf("commands %v", *cmds)
	}
	if _, err := os.Stat(stageDir); !os.IsNotExist(err) {
		t.Fatalf("staging dir not cleaned up: %v", err)
	}
}

func TestSessionFileDownloadRemoteReportsPath(t *testing.T) {
	fake := stdFake()
	transferFake(fake, "4", func(cmd string) []string {
		return []string{"[*] Downloading: /remote/f.txt -> /msf/loot/f.txt\n[*] Completed  : /remote/f.txt -> /msf/loot/f.txt\n"}
	})
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "10.9.9.9"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	if e.State().Connection.FileStaging != "remote" {
		t.Fatalf("fileStaging %q", e.State().Connection.FileStaging)
	}
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileDownload, json.RawMessage(
		`{"sid":"4","path":"/remote/f.txt"}`))
	if eb != nil {
		t.Fatalf("remote download: %+v", eb)
	}
	var payload protocol.FileTransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DataBase64 != "" || payload.SavedPath != "/msf/loot/f.txt" {
		t.Fatalf("remote download must report the host path, not bytes: %+v", payload)
	}
}

func TestSessionFileUploadStagesAndPushes(t *testing.T) {
	fake := stdFake()
	content := []byte("upload me\x00")
	var stagedPath string
	var seen []byte
	cmds := transferFake(fake, "4", func(cmd string) []string {
		fields := strings.Fields(cmd)
		if len(fields) != 3 || fields[0] != "upload" {
			return []string{"[-] unexpected command\n"}
		}
		stagedPath = fields[1]
		seen, _ = os.ReadFile(stagedPath)
		return []string{
			"[*] Uploading  : " + stagedPath + " -> " + fields[2] + "\n" +
				"[*] Uploaded 5.00 B of 10.00 B (50.0%): " + stagedPath + " -> " + fields[2] + "\n",
			"[*] Completed  : " + stagedPath + " -> " + fields[2] + "\n",
		}
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileUpload, json.RawMessage(
		`{"sid":"4","path":"/remote/dir/file.bin","dataBase64":"`+base64.StdEncoding.EncodeToString(content)+`"}`))
	if eb != nil {
		t.Fatalf("file_upload: %+v", eb)
	}
	var payload protocol.SessionRunPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if string(seen) != string(content) {
		t.Fatalf("staged content %q", seen)
	}
	if !strings.Contains(payload.Output, "Completed") {
		t.Fatalf("upload returned before Completed: %q", payload.Output)
	}
	if len(*cmds) != 1 || !strings.HasSuffix((*cmds)[0], " /remote/dir/file.bin") {
		t.Fatalf("commands %v", *cmds)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("staged file not cleaned up: %v", err)
	}
}

func TestSessionFileUploadRemoteRefused(t *testing.T) {
	fake := stdFake()
	meterpreterFake(fake, "4", func(cmd string) string { return "" })
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: time.Hour, RefreshInterval: time.Hour, RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "10.9.9.9"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	_, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileUpload, json.RawMessage(
		`{"sid":"4","path":"/remote/dir","dataBase64":"aGk="}`))
	if eb == nil || !strings.Contains(eb.Message, "--staging-dir") {
		t.Fatalf("remote upload must explain the shared-filesystem requirement: %+v", eb)
	}
}

func TestSessionScreenshotStagesImage(t *testing.T) {
	fake := stdFake()
	jpeg := []byte("\xff\xd8\xff\xe0fake-jpeg-bytes")
	var savedPath string
	transferFake(fake, "4", func(cmd string) []string {
		if !strings.HasPrefix(cmd, "screenshot -p ") {
			return []string{"[-] unexpected command\n"}
		}
		savedPath = strings.TrimPrefix(cmd, "screenshot -p ")
		if err := os.WriteFile(savedPath, jpeg, 0o600); err != nil {
			return []string{"[-] " + err.Error() + "\n"}
		}
		return []string{"Screenshot saved to: " + savedPath + "\n"}
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["4"] != nil })
	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionScreenshot, json.RawMessage(`{"sid":"4"}`))
	if eb != nil {
		t.Fatalf("screenshot: %+v", eb)
	}
	var payload protocol.FileTransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.DataBase64)
	if err != nil || string(decoded) != string(jpeg) {
		t.Fatalf("image round-trip: %q (%v)", decoded, err)
	}
	if _, err := os.Stat(savedPath); !os.IsNotExist(err) {
		t.Fatalf("staged screenshot not cleaned up: %v", err)
	}
}

func TestSessionToolsRejectShellSessions(t *testing.T) {
	fake := stdFake()
	fake.set(gomsf.SessionList, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"5": map[string]interface{}{"type": "shell", "target_host": "10.0.0.6"},
		}, nil
	})
	e := toolsEngine(t, fake)
	waitFor(t, func() bool { return e.State().Sessions["5"] != nil })
	_, eb := e.Exec(context.Background(), "", protocol.MethodSessionFsList, json.RawMessage(`{"sid":"5"}`))
	if eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("shell fs_list: %+v", eb)
	}
}
