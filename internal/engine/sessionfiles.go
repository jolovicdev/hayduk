package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Browser file transfers stage through a directory this process and msfrpcd
// both see. msfrpcd has no RPC to read or write its own disk, so a remote
// daemon cannot hand bytes back: those links keep the pure-RPC tools and
// downloads report the path on the msfrpcd host instead.

// maxTransferBytes caps one staged file so a huge remote file cannot exhaust
// memory on its way to a browser download.
const maxTransferBytes = 64 << 20

// beginTool prepares one meterpreter tool call: toolsMu held and the monitor
// unwatched so the tool read and the live stream never race as two readers on
// one channel. The returned func restores both on every path.
func (e *Engine) beginTool(sid string) (*gomsf.MeterpreterSession, func(), *protocol.ErrorBody) {
	e.toolsMu.Lock()
	e.mu.Lock()
	rpc := e.rpc
	session := e.sessions[sid]
	_, attached := e.interactBufs[sid]
	mon := e.monitor
	e.mu.Unlock()
	if rpc == nil {
		e.toolsMu.Unlock()
		return nil, nil, notConnected()
	}
	if session == nil {
		e.toolsMu.Unlock()
		return nil, nil, &protocol.ErrorBody{Code: protocol.CodeSessionNotFound, Message: "no session " + sid}
	}
	if session.Type != "meterpreter" {
		e.toolsMu.Unlock()
		return nil, nil, badParam("session tools need a meterpreter session; use Interact for shells")
	}
	if attached && mon != nil {
		mon.UnwatchSession(sid)
	}
	done := func() {
		if attached && mon != nil {
			e.mu.Lock()
			_, stillAttached := e.interactBufs[sid]
			sameMon := e.monitor == mon
			e.mu.Unlock()
			if stillAttached && sameMon {
				mon.WatchSession(sid)
			}
		}
		e.toolsMu.Unlock()
	}
	return gomsf.NewMeterpreterSession(rpc, sid), done, nil
}

// msfHostIsLocal reports whether the msfrpcd host names this machine. Only
// literal loopback addresses and localhost qualify: any other name could be
// a remote box, and staging through a directory it cannot see would strand
// files silently.
func msfHostIsLocal(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().IsLoopback()
	}
	return false
}

// prepareStagingDir creates the staging directory and verifies it is private
// to this user. A predictable path in a world-writable parent can be claimed
// by another local user, so a directory we do not own or that others can
// write is refused rather than used.
func prepareStagingDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s is accessible to other users; chmod 700 it", dir)
		}
		if uid := os.Getuid(); uid >= 0 && info.Sys() != nil {
			if owner := fileOwnerUID(info); owner >= 0 && owner != uid {
				return fmt.Errorf("%s is owned by another user", dir)
			}
		}
	}
	return nil
}

// resolveStaging picks the staging directory for a connection. An explicit
// --staging-dir asserts a shared filesystem (a bind mount into a containerized
// msfrpcd, for example); otherwise a loopback daemon gets a private temp dir.
func resolveStaging(explicit, host string) (dir string, auto bool, err error) {
	if explicit != "" {
		if err := prepareStagingDir(explicit); err != nil {
			return "", false, err
		}
		return explicit, false, nil
	}
	if !msfHostIsLocal(host) {
		return "", false, nil
	}
	dir, err = os.MkdirTemp("", "hayduk-staging-")
	if err != nil {
		return "", false, err
	}
	return dir, true, nil
}

func randomName() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// stagingPath returns a fresh private file path inside the staging directory,
// or "" when the link has no staging. Callers hold e.mu or read the snapshot.
func (e *Engine) stagingPath() string {
	e.mu.Lock()
	dir := e.stagingDir
	e.mu.Unlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, randomName())
}

// withinDir reports whether path lies strictly inside dir.
func withinDir(dir, path string) bool {
	if dir == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// withinStaging reports whether a path the daemon reported lies inside the
// staging directory: only those files are read back into a browser reply.
func (e *Engine) withinStaging(path string) bool {
	e.mu.Lock()
	dir := e.stagingDir
	e.mu.Unlock()
	return withinDir(dir, path)
}

func (e *Engine) sessionFsList(ctx context.Context, p protocol.SessionFsListParams) (json.RawMessage, *protocol.ErrorBody) {
	ms, done, eb := e.beginTool(p.SID)
	if eb != nil {
		return nil, eb
	}
	defer done()
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	path := strings.TrimSpace(p.Path)
	if path == "" {
		out, err := ms.RunSingle(rctx, "pwd")
		if err != nil {
			return nil, mapErr(err)
		}
		path = strings.TrimSpace(cleanOutput(out))
	}
	out, err := ms.RunSingle(rctx, "ls "+consoleArg(path))
	if err != nil {
		return nil, mapErr(err)
	}
	listing := cleanOutput(out)
	entries, listedPath, perr := parseFsListing(listing)
	if perr != "" {
		return nil, badParam(perr)
	}
	if listedPath != "" {
		path = listedPath
	}
	sep, err := ms.DirectorySeparator(rctx)
	if err != nil || sep == "" {
		sep = "/"
		if strings.Contains(path, "\\") {
			sep = "\\"
		}
	}
	return mustJSON(protocol.FsListPayload{Path: path, Separator: sep, Entries: entries}), nil
}

// parseFsListing pulls structured rows out of meterpreter's ls table:
// Mode Size Type Last-modified Name, where Type is "dir" or "fil" and the
// modified column spans date, time and zone. The "Listing:" header carries
// the path the session resolved. "." and ".." never appear: ls omits them.
func parseFsListing(out string) (entries []protocol.FsEntry, listedPath, errText string) {
	var errLines []string
	inTable := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "Listing:"); ok {
			listedPath = strings.TrimSpace(rest)
			continue
		}
		if strings.HasPrefix(line, "[-]") {
			errLines = append(errLines, line)
			continue
		}
		if strings.HasPrefix(line, "No entries exist") {
			return nil, listedPath, ""
		}
		fields := strings.Fields(line)
		if fields[0] == "Mode" {
			inTable = true
			continue
		}
		if !inTable || len(fields) < 4 || strings.HasPrefix(fields[0], "-") {
			continue
		}
		kind := ""
		switch fields[2] {
		case "dir":
			kind = "dir"
		case "fil":
			kind = "file"
		default:
			continue
		}
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		var name, modified string
		if dateIdx := findDateField(fields); dateIdx >= 0 && dateIdx+3 <= len(fields) {
			modified = strings.Join(fields[dateIdx:dateIdx+3], " ")
			name = strings.Join(fields[dateIdx+3:], " ")
		} else {
			name = strings.Join(fields[3:], " ")
		}
		if name == "" {
			continue
		}
		entries = append(entries, protocol.FsEntry{Name: name, Size: size, Kind: kind, Modified: modified})
	}
	if len(entries) == 0 && len(errLines) > 0 {
		return nil, listedPath, strings.Join(errLines, " ")
	}
	if !inTable && len(entries) == 0 && listedPath == "" && len(errLines) == 0 && strings.TrimSpace(out) != "" {
		return nil, "", "could not read a directory listing from: " + firstLine(out)
	}
	return entries, listedPath, ""
}

func findDateField(fields []string) int {
	for i, f := range fields {
		if len(f) == 10 && f[4] == '-' && f[7] == '-' {
			if _, err := time.Parse("2006-01-02", f); err == nil {
				return i
			}
		}
	}
	return -1
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 160 {
		line = line[:160] + "…"
	}
	return line
}

func hasErrorLine(s string) bool {
	for _, raw := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "[-]") {
			return true
		}
	}
	return false
}

// errorLine returns the first Metasploit [-] error line in s, falling back to
// firstLine so callers surface the actual failure instead of a banner or echo.
func errorLine(s string) string {
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[-]") {
			return firstLine(line)
		}
	}
	return firstLine(s)
}

// remoteBaseName trims the last segment of a path that may use either
// separator: the remote OS decides, not the host running Hayduk.
func remoteBaseName(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// findStagedDownload locates the downloaded file inside stageDir, preferring
// the path meterpreter printed when it stays inside stageDir.
func findStagedDownload(stageDir, listing string) string {
	if dst := transferDestPath(listing); withinDir(stageDir, dst) {
		if info, err := os.Lstat(dst); err == nil && info.Mode().IsRegular() {
			return dst
		}
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			return filepath.Join(stageDir, entry.Name())
		}
	}
	return ""
}

func (e *Engine) sessionFileDownload(ctx context.Context, p protocol.SessionFileRefParams) (json.RawMessage, *protocol.ErrorBody) {
	if strings.TrimSpace(p.Path) == "" {
		return nil, badParam("path is required")
	}
	// Meterpreter's file.download appends File.basename(src) whenever the
	// destination basename differs from the source, so pass a fresh per-transfer
	// directory instead of a flat file path.
	stageDir := e.stagingPath()
	if stageDir != "" {
		if err := os.Mkdir(stageDir, 0o700); err != nil {
			return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: err.Error()}
		}
		defer os.RemoveAll(stageDir)
	}
	ms, done, eb := e.beginTool(p.SID)
	if eb != nil {
		return nil, eb
	}
	defer done()

	cmd := "download " + consoleArg(p.Path)
	if stageDir != "" {
		cmd += " " + consoleArg(stageDir)
	}
	// Wait for the transfer's terminal status line, not "Downloaded" (which
	// fires mid-transfer on multi-MB progress updates and never fires on empty
	// files).
	out, err := ms.RunWithOutput(ctx, cmd, []string{
		"Completed", "Overwritten", "Skipped", "Downloaded  :",
		"[-]", "No matching files", "Error downloading - giving up", "Error opening",
	}, 120*time.Second)
	if err != nil {
		return nil, mapErr(err)
	}
	listing := cleanOutput(out)
	if strings.Contains(listing, "[-]") || strings.Contains(listing, "No matching files") {
		return nil, badParam(errorLine(listing))
	}
	if stageDir == "" {
		return mustJSON(protocol.FileTransferPayload{
			Filename:  remoteBaseName(p.Path),
			SavedPath: transferDestPath(listing),
			Output:    listing,
		}), nil
	}
	landed := findStagedDownload(stageDir, listing)
	if landed == "" {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: "the transfer did not land in the staging directory: " + firstLine(listing)}
	}
	data, rerr := readStagedFile(landed)
	if rerr != nil {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: rerr.Error()}
	}
	return mustJSON(protocol.FileTransferPayload{
		Filename:   remoteBaseName(p.Path),
		DataBase64: base64.StdEncoding.EncodeToString(data),
		Size:       len(data),
	}), nil
}

// transferDestPath reads the local side of the last "src -> dst" status line
// that meterpreter prints for a download landing on the msfrpcd host.
func transferDestPath(out string) string {
	dest := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "->") {
			continue
		}
		if _, after, ok := strings.Cut(line, "->"); ok {
			if trimmed := strings.TrimSpace(after); trimmed != "" {
				dest = trimmed
			}
		}
	}
	return dest
}

func readStagedFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("the transfer did not land in the staging directory: %w", err)
	}
	if info.Size() > maxTransferBytes {
		return nil, fmt.Errorf("file is %d bytes; browser transfers are capped at %d", info.Size(), maxTransferBytes)
	}
	return os.ReadFile(path)
}

func (e *Engine) sessionFileUpload(ctx context.Context, p protocol.SessionFileUploadParams) (json.RawMessage, *protocol.ErrorBody) {
	if strings.TrimSpace(p.Path) == "" {
		return nil, badParam("path is required")
	}
	staged := e.stagingPath()
	if staged == "" {
		return nil, badParam("msfrpcd is remote: browser uploads need a shared filesystem (--staging-dir)")
	}
	data, err := base64.StdEncoding.DecodeString(p.DataBase64)
	if err != nil {
		return nil, badParam("dataBase64 is not valid base64")
	}
	if len(data) == 0 {
		return nil, badParam("nothing to upload")
	}
	if len(data) > maxTransferBytes {
		return nil, badParam(fmt.Sprintf("upload is %d bytes; browser transfers are capped at %d", len(data), maxTransferBytes))
	}
	if err := os.WriteFile(staged, data, 0o600); err != nil {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: err.Error()}
	}
	defer os.Remove(staged)

	ms, done, eb := e.beginTool(p.SID)
	if eb != nil {
		return nil, eb
	}
	defer done()
	out, err := ms.RunWithOutput(ctx, "upload "+consoleArg(staged)+" "+consoleArg(p.Path),
		[]string{"Completed", "Uploaded   :", "[-]"}, 120*time.Second)
	if err != nil {
		return nil, mapErr(err)
	}
	listing := cleanOutput(out)
	if strings.Contains(listing, "[-]") {
		return nil, badParam(errorLine(listing))
	}
	return mustJSON(protocol.SessionRunPayload{Output: listing}), nil
}

func (e *Engine) sessionScreenshot(ctx context.Context, p protocol.SessionFileRefParams) (json.RawMessage, *protocol.ErrorBody) {
	staged := e.stagingPath()
	if staged != "" {
		defer os.Remove(staged + ".jpeg")
	}
	ms, done, eb := e.beginTool(p.SID)
	if eb != nil {
		return nil, eb
	}
	defer done()

	cmd := "screenshot"
	if staged != "" {
		cmd += " -p " + consoleArg(staged+".jpeg")
	}
	out, err := ms.RunWithOutput(ctx, cmd, []string{"Screenshot saved to", "[-]"}, 60*time.Second)
	if err != nil {
		return nil, mapErr(err)
	}
	listing := cleanOutput(out)
	if strings.Contains(listing, "[-]") {
		return nil, badParam(errorLine(listing))
	}
	saved := screenshotSavedPath(listing)
	if staged == "" || saved == "" || !e.withinStaging(saved) {
		return mustJSON(protocol.FileTransferPayload{SavedPath: saved, Output: listing}), nil
	}
	defer os.Remove(saved)
	data, rerr := readStagedFile(saved)
	if rerr != nil {
		return nil, &protocol.ErrorBody{Code: protocol.CodeInternal, Message: rerr.Error()}
	}
	return mustJSON(protocol.FileTransferPayload{
		Filename:   filepath.Base(saved),
		DataBase64: base64.StdEncoding.EncodeToString(data),
		Size:       len(data),
		Output:     listing,
	}), nil
}

func screenshotSavedPath(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if _, after, ok := strings.Cut(line, "Screenshot saved to:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}
